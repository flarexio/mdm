package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"
	"go.uber.org/zap"

	"github.com/flarexio/core/events"
	"github.com/flarexio/core/policy"
	"github.com/flarexio/mdm"
	"github.com/flarexio/mdm/auth"
	"github.com/flarexio/mdm/conf"
	"github.com/flarexio/mdm/identity"
	"github.com/flarexio/mdm/persistence/inmem"
	"github.com/flarexio/mdm/push"

	badgerdb "github.com/flarexio/mdm/persistence/badger"
	transhttp "github.com/flarexio/mdm/transport/http"
)

var (
	Version   string = "0.0.0"
	BuildTime string
	GitCommit string
)

var versionCmd = &cli.Command{
	Name:    "version",
	Aliases: []string{"ver", "v"},
	Usage:   "Show version",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "all",
			Aliases: []string{"a"},
			Usage:   "Show all information (include: Version, BuildTime, GitCommit)",
			Value:   false,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		if !cmd.Bool("all") {
			fmt.Println(cmd.Root().Version)
		} else {
			cli.ShowVersion(cmd)
		}
		return nil
	},
}

func main() {
	cli.VersionPrinter = func(cmd *cli.Command) {
		fmt.Println("Version: " + cmd.Root().Version)
		fmt.Println("BuildTime: " + BuildTime)
		fmt.Println("GitCommit: " + GitCommit)
	}

	cmd := &cli.Command{
		Name:     "mdm-server",
		Usage:    "A minimal Apple MDM server (learning vehicle, NanoMDM-aligned)",
		Version:  Version,
		Commands: []*cli.Command{versionCmd},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "path",
				Usage:   "Working directory for config and certs (default ~/.flarex/mdm)",
				Sources: cli.EnvVars("MDM_PATH"),
			},
			&cli.IntFlag{
				Name:    "port",
				Usage:   "HTTP port for the admin/integration server (health, enrollment)",
				Value:   8080,
				Sources: cli.EnvVars("MDM_HTTP_PORT"),
			},
			&cli.BoolFlag{
				Name:    "mtls-enabled",
				Usage:   "Serve the device endpoints (/checkin, /server) over mTLS",
				Value:   false,
				Sources: cli.EnvVars("MDM_MTLS_ENABLED"),
			},
			&cli.IntFlag{
				Name:    "mtls-port",
				Usage:   "Port for the device-facing mTLS server",
				Value:   8443,
				Sources: cli.EnvVars("MDM_MTLS_PORT"),
			},
		},
		Action: run,
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

// run is the composition root: every interface is bound to a concrete
// implementation here, at the outermost layer, and the HTTP servers are assembled
// and started.
func run(ctx context.Context, cmd *cli.Command) error {
	logger, err := zap.NewDevelopment()
	if err != nil {
		return err
	}
	defer logger.Sync()
	zap.ReplaceGlobals(logger)

	path := cmd.String("path")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path = filepath.Join(home, ".flarex", "mdm")
	}

	cfg, err := conf.LoadConfig(path)
	if err != nil {
		return err
	}

	// Infrastructure. Enrollments are durable (BadgerDB) so they survive a restart;
	// the command queue is still in-memory (a lost command is simply re-enqueued).
	enrollments, err := badgerdb.NewEnrollmentRepository(filepath.Join(path, "db"))
	if err != nil {
		return err
	}
	defer enrollments.Close()

	// Strong-consistency bridge over the durable repo's lag (see enrollment.Cache);
	// Redis in a scaled deployment.
	cache, err := inmem.NewEnrollmentCache()
	if err != nil {
		return err
	}
	defer cache.Close()

	commands, err := inmem.NewCommandQueue()
	if err != nil {
		return err
	}

	pusher, topic, err := buildPusher(cfg, logger)
	if err != nil {
		return err
	}

	// Event sourcing: check-in methods Notify(), the subscribed handler does the
	// durable Store. Synchronous pubsub single-node; NATS in a scaled deployment.
	ps := inmem.NewPubSub()
	events.ReplaceGlobals(ps)

	// The core service: the composition of all the layers, wrapped in the logging
	// middleware so every service call is traced.
	svc := mdm.NewService(enrollments, cache, commands, pusher)
	svc = mdm.LoggingMiddleware(logger)(svc)

	handler, err := svc.Handler()
	if err != nil {
		return err
	}
	if err := mdm.RegisterEventHandler(ps, handler); err != nil {
		return err
	}

	enroller, err := buildEnroller(cfg, topic)
	if err != nil {
		return err
	}
	enroller = mdm.EnrollerLoggingMiddleware(logger)(enroller)

	authz, err := buildAuthz(ctx, cfg, buildVerifier(cfg, logger), logger)
	if err != nil {
		return err
	}

	// Admin / integration server (no mTLS).
	admin := http.NewServeMux()
	admin.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Self-service enrollment (user role): the subject is taken from the caller's
	// token (claims.sub), not the URL, so a user can only enroll themselves.
	admin.Handle("POST /enroll", authz("mdm::enroll.issue")(transhttp.EnrollHandler(enroller)))

	// Operator endpoints (admin role).
	admin.Handle("POST /enqueue/{subject}", authz("mdm::commands.enqueue")(transhttp.EnqueueHandler(svc)))
	admin.Handle("GET /enrollments", authz("mdm::enrollments.list")(transhttp.EnrollmentsHandler(svc)))
	admin.Handle("GET /enrollments/{subject}", authz("mdm::enrollments.read")(transhttp.EnrollmentHandler(svc)))

	adminSrv := &http.Server{Addr: fmt.Sprintf(":%d", cmd.Int("port")), Handler: admin}
	go serve(logger, "admin", func() error { return adminSrv.ListenAndServe() })

	// Device-facing MDM server (mTLS): the check-in and command channels.
	var deviceSrv *http.Server
	if cmd.Bool("mtls-enabled") {
		deviceSrv, err = buildDeviceServer(resolve(cfg.Path, cfg.CA), cmd.Int("mtls-port"), svc)
		if err != nil {
			return err
		}

		certFile := filepath.Join(path, "certs", "server.crt")
		keyFile := filepath.Join(path, "certs", "server.key")
		go serve(logger, "device(mTLS)", func() error { return deviceSrv.ListenAndServeTLS(certFile, keyFile) })
	} else {
		logger.Warn("mTLS disabled; device endpoints (/checkin, /server) are not served")
	}

	// Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sign := <-quit
	logger.Info("shutdown", zap.String("signal", sign.String()))

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_ = adminSrv.Shutdown(shutdownCtx)
	if deviceSrv != nil {
		_ = deviceSrv.Shutdown(shutdownCtx)
	}

	return nil
}

// buildPusher builds the certificate-based APNs client from the push certificate.
// Push is optional for now (the command channel that uses it is disabled): with no
// certificate it returns a no-op pusher and an empty topic.
func buildPusher(cfg *conf.Config, logger *zap.Logger) (push.Pusher, string, error) {
	if cfg.Push.Cert == "" {
		logger.Warn("no push certificate configured; APNs push is disabled")
		return noopPusher{}, "", nil
	}

	cert, err := tls.LoadX509KeyPair(resolve(cfg.Path, cfg.Push.Cert), resolve(cfg.Path, cfg.Push.Key))
	if err != nil {
		return nil, "", fmt.Errorf("loading push certificate: %w", err)
	}

	topic, err := push.TopicFromCertificate(cert)
	if err != nil {
		return nil, "", fmt.Errorf("reading push topic: %w", err)
	}

	logger.Info("APNs push enabled", zap.String("topic", topic))
	return push.NewCertClient(push.HostProduction, cert), topic, nil
}

// buildEnroller builds the enroller that mints enrollment profiles, fetching SCEP
// challenges from identity over mTLS. It also embeds the FlareX root (the
// self-signed cert in cfg.CA) as a trust anchor in the profile so the device
// trusts the private MDM/SCEP TLS certs; the anchor is skipped (with a warning) if
// no root is found.
func buildEnroller(cfg *conf.Config, topic string) (mdm.Enroller, error) {
	cert, roots, err := identityMTLS(cfg)
	if err != nil {
		return nil, err
	}

	rootCA, err := rootCertDER(resolve(cfg.Path, cfg.CA))
	if err != nil {
		zap.L().Warn("no root trust anchor; enrollment profile will omit it", zap.Error(err))
	}

	enrollCfg := mdm.EnrollConfig{
		Identifier:   cfg.Enroll.Identifier,
		Organization: cfg.Enroll.Organization,
		SCEPURL:      cfg.Enroll.SCEP.URL,
		CAName:       cfg.Enroll.SCEP.CAName,
		ServerURL:    cfg.Enroll.ExternalURL + "/server",
		CheckInURL:   cfg.Enroll.ExternalURL + "/checkin",
		Topic:        topic,
		RootCA:       rootCA,
	}

	challenger := identity.NewClient(cfg.Identity.URL, cert, roots)
	return mdm.NewEnroller(challenger, enrollCfg), nil
}

// rootCertDER returns the DER of the self-signed (root) certificate from a PEM
// file that may hold a chain — that is the trust anchor, regardless of order.
func rootCertDER(path string) ([]byte, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	for {
		var block *pem.Block
		block, pemBytes = pem.Decode(pemBytes)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		if bytes.Equal(cert.RawSubject, cert.RawIssuer) {
			return block.Bytes, nil // self-signed = root
		}
	}

	return nil, fmt.Errorf("no self-signed root certificate in %s", path)
}

// buildVerifier builds the JWKS-backed bearer-token verifier. The JWKS is public
// data served over identity's public (Let's Encrypt) endpoint, so it is fetched
// over normal system-trust TLS — no client certificate needed.
func buildVerifier(cfg *conf.Config, logger *zap.Logger) auth.Verifier {
	client := &http.Client{Timeout: 10 * time.Second}
	logger.Info("admin token verification enabled", zap.String("jwks", cfg.Auth.JWKSURL))
	return auth.NewJWKSVerifier(cfg.Auth.JWKSURL, cfg.Auth.Issuer, cfg.Auth.Audience, client)
}

// buildAuthz loads the OPA policy from <path>/permissions.json and returns an
// authorizator bound to the verifier.
func buildAuthz(ctx context.Context, cfg *conf.Config, verifier auth.Verifier, logger *zap.Logger) (transhttp.Authz, error) {
	pol, err := policy.NewRegoPolicy(ctx, resolve(cfg.Path, "permissions.json"))
	if err != nil {
		return nil, fmt.Errorf("loading authorization policy: %w", err)
	}

	logger.Info("admin endpoints require a bearer token and OPA authorization")
	return transhttp.Authorizator(verifier, pol), nil
}

// identityMTLS loads the client certificate and trust roots used to reach
// identity's mTLS challenge-generation endpoint.
func identityMTLS(cfg *conf.Config) (tls.Certificate, *x509.CertPool, error) {
	cert, err := tls.LoadX509KeyPair(resolve(cfg.Path, cfg.Identity.Cert), resolve(cfg.Path, cfg.Identity.Key))
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("loading identity client certificate: %w", err)
	}

	caPEM, err := os.ReadFile(resolve(cfg.Path, cfg.CA))
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("reading CA: %w", err)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return tls.Certificate{}, nil, errors.New("no certificates found in identity CA file")
	}

	return cert, roots, nil
}

// resolve joins a config-relative path against the working directory.
func resolve(base, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

// buildDeviceServer assembles the mTLS server that verifies device certificates
// against the CA in caFile and serves the two MDM channels.
func buildDeviceServer(caFile string, port int, svc mdm.Service) (*http.Server, error) {
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("reading device CA: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("no certificates found in device CA file")
	}

	requireID := transhttp.RequireIdentity(transhttp.ClientIdentity)

	mux := http.NewServeMux()
	mux.Handle("PUT /checkin", requireID(transhttp.CheckInHandler(svc)))
	mux.Handle("PUT /server", requireID(transhttp.CommandHandler(svc)))

	return &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
		TLSConfig: &tls.Config{
			ClientCAs:  pool,
			ClientAuth: tls.RequireAndVerifyClientCert,
			MinVersion: tls.VersionTLS12,
		},
	}, nil
}

func serve(logger *zap.Logger, name string, listen func() error) {
	logger.Info("server listening", zap.String("server", name))
	if err := listen(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped", zap.String("server", name), zap.Error(err))
	}
}

// noopPusher is used while push is deferred: the command channel that would call
// it is disabled, so it never sends.
type noopPusher struct{}

func (noopPusher) Push(context.Context, push.Target) error { return nil }

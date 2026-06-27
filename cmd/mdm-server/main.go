package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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

	"github.com/flarexio/mdm"
	"github.com/flarexio/mdm/persistence/inmem"
	"github.com/flarexio/mdm/push"

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
				Usage:   "Working directory (mTLS certs are read from <path>/certs)",
				Sources: cli.EnvVars("MDM_PATH"),
			},
			&cli.IntFlag{
				Name:    "port",
				Usage:   "HTTP port for the admin/integration server (webhook, health)",
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
			&cli.StringFlag{
				Name:    "push-cert",
				Usage:   "Path to the MDM push certificate (PEM); enables real APNs",
				Sources: cli.EnvVars("MDM_PUSH_CERT"),
			},
			&cli.StringFlag{
				Name:    "push-key",
				Usage:   "Path to the MDM push certificate key (PEM)",
				Sources: cli.EnvVars("MDM_PUSH_KEY"),
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

	// Infrastructure. In-memory today; a durable/shared backend slots in behind the
	// same interfaces (see the persistence package).
	enrollments, err := inmem.NewEnrollmentRepository()
	if err != nil {
		return err
	}
	defer enrollments.Close()

	commands, err := inmem.NewCommandQueue()
	if err != nil {
		return err
	}

	pusher, err := buildPusher(cmd, logger)
	if err != nil {
		return err
	}

	// The core service: the composition of all the layers.
	svc := mdm.NewService(enrollments, commands, pusher)

	// Admin / integration server (no mTLS): health.
	admin := http.NewServeMux()
	admin.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	adminSrv := &http.Server{Addr: fmt.Sprintf(":%d", cmd.Int("port")), Handler: admin}
	go serve(logger, "admin", func() error { return adminSrv.ListenAndServe() })

	// Device-facing MDM server (mTLS): the check-in and command channels.
	var deviceSrv *http.Server
	if cmd.Bool("mtls-enabled") {
		deviceSrv, err = buildDeviceServer(cmd, svc)
		if err != nil {
			return err
		}

		base := cmd.String("path")
		certFile := filepath.Join(base, "certs", "server.crt")
		keyFile := filepath.Join(base, "certs", "server.key")
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

// buildPusher returns a certificate-based APNs client when a push certificate is
// configured, otherwise a logging no-op so the server still runs for development.
func buildPusher(cmd *cli.Command, logger *zap.Logger) (push.Pusher, error) {
	certPath, keyPath := cmd.String("push-cert"), cmd.String("push-key")
	if certPath == "" || keyPath == "" {
		logger.Warn("no MDM push certificate configured; using a logging no-op pusher")
		return logPusher{logger}, nil
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("loading push certificate: %w", err)
	}

	topic, err := push.TopicFromCertificate(cert)
	if err != nil {
		return nil, fmt.Errorf("reading push topic: %w", err)
	}

	logger.Info("APNs push enabled", zap.String("topic", topic))
	return push.NewCertClient(push.HostProduction, cert), nil
}

// buildDeviceServer assembles the mTLS server that verifies device certificates
// against the CA in <path>/certs/ca.crt and serves the two MDM channels.
func buildDeviceServer(cmd *cli.Command, svc mdm.Service) (*http.Server, error) {
	base := cmd.String("path")

	caPEM, err := os.ReadFile(filepath.Join(base, "certs", "ca.crt"))
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
		Addr:    fmt.Sprintf(":%d", cmd.Int("mtls-port")),
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

// logPusher is a Pusher that logs instead of contacting APNs, used when no push
// certificate is configured.
type logPusher struct{ logger *zap.Logger }

func (p logPusher) Push(_ context.Context, t push.Target) error {
	p.logger.Info("APNs push (no certificate configured; not sent)",
		zap.String("topic", t.Topic),
		zap.String("token", t.Token),
	)
	return nil
}

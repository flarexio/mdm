package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
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
				Usage:   "Specifies the working directory",
				Sources: cli.EnvVars("MDM_PATH"),
			},
			&cli.IntFlag{
				Name:    "port",
				Usage:   "Specifies the HTTP service port",
				Value:   8080,
				Sources: cli.EnvVars("MDM_HTTP_PORT"),
			},
			&cli.BoolFlag{
				Name:    "mtls-enabled",
				Usage:   "Enable mTLS service (devices authenticate with their SCEP identity cert)",
				Value:   false,
				Sources: cli.EnvVars("MDM_MTLS_ENABLED"),
			},
			&cli.IntFlag{
				Name:    "mtls-port",
				Usage:   "Specifies the mTLS service port",
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

// run is the composition root: it wires repository -> service -> middleware ->
// endpoint -> transport. For now (module 0) it only boots the logger and blocks
// until a shutdown signal; each later module fills in one layer.
func run(ctx context.Context, cmd *cli.Command) error {
	log, err := zap.NewDevelopment()
	if err != nil {
		return err
	}
	defer log.Sync()

	zap.ReplaceGlobals(log)

	log.Info("mdm-server starting (skeleton)",
		zap.String("version", Version),
		zap.Int("port", int(cmd.Int("port"))),
		zap.Bool("mtls_enabled", cmd.Bool("mtls-enabled")),
	)

	// TODO(module 1+): wire persistence, service, endpoints, transports here.

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	sign := <-quit

	log.Info("shutdown", zap.String("signal", sign.String()))
	return nil
}

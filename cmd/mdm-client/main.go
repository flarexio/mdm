// Command mdm-client is a terminal UI for sending MDM commands to a device and
// watching their results. It enqueues through the mdm admin API and waits for the
// command_responded event on NATS.
package main

import (
	"context"
	"log"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/urfave/cli/v3"

	"github.com/nats-io/nats.go"
)

func main() {
	cmd := &cli.Command{
		Name:  "mdm-client",
		Usage: "Send MDM commands and watch their responses (TUI)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "url",
				Usage:   "mdm admin base URL",
				Value:   "https://mdm.flarex.io",
				Sources: cli.EnvVars("MDM_URL"),
			},
			&cli.StringFlag{
				Name:    "nats",
				Usage:   "NATS server URL",
				Value:   "wss://nats.flarex.io",
				Sources: cli.EnvVars("NATS_URL"),
			},
			&cli.StringFlag{
				Name:    "creds",
				Usage:   "NATS user credentials file",
				Sources: cli.EnvVars("NATS_CREDS"),
			},
			&cli.DurationFlag{
				Name:  "timeout",
				Usage: "how long to wait for a command response",
				Value: 30 * time.Second,
			},
		},
		Action: run,
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, c *cli.Command) error {
	var opts []nats.Option
	if creds := c.String("creds"); creds != "" {
		opts = append(opts, nats.UserCredentials(creds))
	}

	nc, err := nats.Connect(c.String("nats"), opts...)
	if err != nil {
		return err
	}
	defer nc.Drain()

	m := newModel(c.String("url"), nc, c.Duration("timeout"))
	_, err = tea.NewProgram(m, tea.WithContext(ctx)).Run()
	return err
}

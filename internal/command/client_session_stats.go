package command

import (
	"os"

	"github.com/cenkalti/rain/v2/internal/console"
	"github.com/urfave/cli"
)

func clientSessionStatsCommand() cli.Command {
	return cli.Command{
		Name:     "session-stats",
		Usage:    "get stats of session",
		Category: "Getters",
		Action:   handleSessionStats,
		Flags: []cli.Flag{
			cli.BoolFlag{
				Name:  "json",
				Usage: "print raw stats as JSON",
			},
		},
	}
}

func handleSessionStats(c *cli.Context) error {
	s, err := clt.GetSessionStats()
	if err != nil {
		return err
	}
	if c.Bool("json") {
		return printJSON(s)
	}
	console.FormatSessionStats(s, os.Stdout)
	return nil
}

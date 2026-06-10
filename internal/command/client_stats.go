package command

import (
	"os"

	"github.com/cenkalti/rain/v2/internal/console"
	"github.com/urfave/cli"
)

func clientStatsCommand() cli.Command {
	return cli.Command{
		Name:     "stats",
		Usage:    "get stats of torrent",
		Category: "Getters",
		Action:   handleStats,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
			cli.BoolFlag{
				Name:  "json",
				Usage: "print raw stats as JSON",
			},
		},
	}
}

func handleStats(c *cli.Context) error {
	s, err := clt.GetTorrentStats(c.String("id"))
	if err != nil {
		return err
	}
	if c.Bool("json") {
		return printJSON(s)
	}
	console.FormatStats(s, os.Stdout)
	return nil
}

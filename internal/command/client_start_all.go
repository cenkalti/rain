package command

import "github.com/urfave/cli"

func clientStartAllCommand() cli.Command {
	return cli.Command{
		Name:     "start-all",
		Usage:    "start all torrents",
		Category: "Actions",
		Action:   handleStartAll,
	}
}

func handleStartAll(c *cli.Context) error {
	return clt.StartAllTorrents()
}

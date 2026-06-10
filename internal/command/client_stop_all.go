package command

import "github.com/urfave/cli"

func clientStopAllCommand() cli.Command {
	return cli.Command{
		Name:     "stop-all",
		Usage:    "stop all torrents",
		Category: "Actions",
		Action:   handleStopAll,
	}
}

func handleStopAll(c *cli.Context) error {
	return clt.StopAllTorrents()
}

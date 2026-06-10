package command

import "github.com/urfave/cli"

func clientStopCommand() cli.Command {
	return cli.Command{
		Name:     "stop",
		Usage:    "stop torrent",
		Category: "Actions",
		Action:   handleStop,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
		},
	}
}

func handleStop(c *cli.Context) error {
	return clt.StopTorrent(c.String("id"))
}

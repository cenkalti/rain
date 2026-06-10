package command

import "github.com/urfave/cli"

func clientStartCommand() cli.Command {
	return cli.Command{
		Name:     "start",
		Usage:    "start torrent",
		Category: "Actions",
		Action:   handleStart,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
		},
	}
}

func handleStart(c *cli.Context) error {
	return clt.StartTorrent(c.String("id"))
}

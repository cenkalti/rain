package command

import "github.com/urfave/cli"

func clientMoveCommand() cli.Command {
	return cli.Command{
		Name:     "move",
		Usage:    "move torrent to another server",
		Category: "Actions",
		Action:   handleMove,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
			cli.StringFlag{
				Name:     "target",
				Required: true,
				Usage:    "target server in host:port format",
			},
		},
	}
}

func handleMove(c *cli.Context) error {
	return clt.MoveTorrent(c.String("id"), c.String("target"))
}

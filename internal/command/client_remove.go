package command

import "github.com/urfave/cli"

func clientRemoveCommand() cli.Command {
	return cli.Command{
		Name:     "remove",
		Usage:    "remove torrent",
		Category: "Actions",
		Action:   handleRemove,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
			cli.BoolFlag{
				Name: "keep-data",
			},
		},
	}
}

func handleRemove(c *cli.Context) error {
	return clt.RemoveTorrent(c.String("id"), c.Bool("keep-data"))
}

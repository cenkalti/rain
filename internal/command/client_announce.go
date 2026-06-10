package command

import "github.com/urfave/cli"

func clientAnnounceCommand() cli.Command {
	return cli.Command{
		Name:     "announce",
		Usage:    "announce to tracker",
		Category: "Actions",
		Action:   handleAnnounce,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
		},
	}
}

func handleAnnounce(c *cli.Context) error {
	return clt.AnnounceTorrent(c.String("id"))
}

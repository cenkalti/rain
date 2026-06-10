package command

import "github.com/urfave/cli"

func clientAddPeerCommand() cli.Command {
	return cli.Command{
		Name:     "add-peer",
		Usage:    "add peer to torrent",
		Category: "Actions",
		Action:   handleAddPeer,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
			cli.StringFlag{
				Name:     "addr",
				Usage:    "peer address in host:port format",
				Required: true,
			},
		},
	}
}

func handleAddPeer(c *cli.Context) error {
	return clt.AddPeer(c.String("id"), c.String("addr"))
}

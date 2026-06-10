package command

import "github.com/urfave/cli"

func clientPeersCommand() cli.Command {
	return cli.Command{
		Name:     "peers",
		Usage:    "get peers of torrent",
		Category: "Getters",
		Action:   handlePeers,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
		},
	}
}

func handlePeers(c *cli.Context) error {
	resp, err := clt.GetTorrentPeers(c.String("id"))
	if err != nil {
		return err
	}
	return printJSON(resp)
}

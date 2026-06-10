package command

import (
	"os"

	"github.com/urfave/cli"
)

func clientSaveTorrentCommand() cli.Command {
	return cli.Command{
		Name:     "torrent",
		Usage:    "save torrent file",
		Category: "Getters",
		Action:   handleSaveTorrent,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
			cli.StringFlag{
				Name:     "out,o",
				Required: true,
			},
		},
	}
}

func handleSaveTorrent(c *cli.Context) error {
	torrent, err := clt.GetTorrent(c.String("id"))
	if err != nil {
		return err
	}
	f, err := os.Create(c.String("out"))
	if err != nil {
		return err
	}
	_, err = f.Write(torrent)
	if err != nil {
		return err
	}
	return f.Close()
}

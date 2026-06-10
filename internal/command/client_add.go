package command

import (
	"os"

	"github.com/cenkalti/rain/v2/rainrpc"
	"github.com/urfave/cli"
)

func clientAddCommand() cli.Command {
	return cli.Command{
		Name:     "add",
		Usage:    "add torrent or magnet",
		Category: "Actions",
		Action:   handleAdd,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "torrent,t",
				Usage:    "file or URI",
				Required: true,
			},
			cli.BoolFlag{
				Name:  "stopped",
				Usage: "do not start torrent automatically",
			},
			cli.BoolFlag{
				Name:  "stop-after-download",
				Usage: "stop the torrent after download is finished",
			},
			cli.BoolFlag{
				Name:  "stop-after-metadata",
				Usage: "stop the torrent after metadata download is finished",
			},
			cli.StringFlag{
				Name:  "id",
				Usage: "if id is not given, a unique id is automatically generated",
			},
		},
	}
}

func handleAdd(c *cli.Context) error {
	arg := c.String("torrent")
	addOpt := &rainrpc.AddTorrentOptions{
		Stopped:           c.Bool("stopped"),
		StopAfterDownload: c.Bool("stop-after-download"),
		StopAfterMetadata: c.Bool("stop-after-metadata"),
		ID:                c.String("id"),
	}
	if isURI(arg) {
		resp, err := clt.AddURI(arg, addOpt)
		if err != nil {
			return err
		}
		return printJSON(resp)
	}
	f, err := os.Open(arg)
	if err != nil {
		return err
	}
	resp, err := clt.AddTorrent(f, addOpt)
	_ = f.Close()
	if err != nil {
		return err
	}
	return printJSON(resp)
}

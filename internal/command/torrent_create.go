package command

import (
	"os"

	"github.com/cenkalti/rain/v2/internal/metainfo"
	"github.com/urfave/cli"
)

func torrentCreateCommand() cli.Command {
	return cli.Command{
		Name:   "create",
		Usage:  "create new torrent file",
		Action: handleTorrentCreate,
		Flags: []cli.Flag{
			cli.StringSliceFlag{
				Name:     "file,f",
				Usage:    "include this file or directory in torrent",
				Required: true,
			},
			cli.StringFlag{
				Name:     "out,o",
				Usage:    "save generated torrent to this `FILE`",
				Required: true,
			},
			cli.StringFlag{
				Name:  "root,r",
				Usage: "file paths given become relative to the root",
			},
			cli.StringFlag{
				Name:  "name,n",
				Usage: "set name of torrent. required if you specify more than one file.",
			},
			cli.BoolFlag{
				Name:  "private,p",
				Usage: "create torrent for private trackers",
			},
			cli.IntFlag{
				Name:  "piece-length,l",
				Usage: "override default piece length. by default, piece length calculated automatically based on the total size of files. given in KB. must be multiple of 16.",
			},
			cli.StringFlag{
				Name:  "comment,c",
				Usage: "add `COMMENT` to torrent",
			},
			cli.StringSliceFlag{
				Name:  "tracker,t",
				Usage: "add tracker `URL`",
			},
			cli.StringSliceFlag{
				Name:  "webseed,w",
				Usage: "add webseed `URL`",
			},
		},
	}
}

func handleTorrentCreate(c *cli.Context) error {
	paths := c.StringSlice("file")
	out := c.String("out")
	root := c.String("root")
	name := c.String("name")
	private := c.Bool("private")
	pieceLength := c.Uint("piece-length")
	comment := c.String("comment")
	trackers := c.StringSlice("tracker")
	webseeds := c.StringSlice("webseed")

	out = os.ExpandEnv(out)
	for i, path := range paths {
		paths[i] = os.ExpandEnv(path)
	}

	tiers := make([][]string, len(trackers))
	for i, tr := range trackers {
		tiers[i] = []string{tr}
	}

	info, err := metainfo.NewInfoBytes(root, paths, private, uint32(pieceLength<<10), name, log)
	if err != nil {
		return err
	}
	mi, err := metainfo.NewBytes(info, tiers, webseeds, comment)
	if err != nil {
		return err
	}
	log.Infof("Created torrent size: %d bytes", len(mi))
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	_, err = f.Write(mi)
	if err != nil {
		return err
	}
	return f.Close()
}

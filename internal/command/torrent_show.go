package command

import (
	"fmt"
	"os"

	"github.com/hokaccha/go-prettyjson"
	"github.com/urfave/cli"
	"github.com/zeebo/bencode"
)

func torrentShowCommand() cli.Command {
	return cli.Command{
		Name:   "show",
		Usage:  "show contents of the torrent file",
		Action: handleTorrentShow,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "file,f",
				Required: true,
			},
		},
	}
}

func handleTorrentShow(c *cli.Context) error {
	f, err := os.Open(c.String("file"))
	if err != nil {
		return err
	}
	defer f.Close()

	val := make(map[string]any)
	err = bencode.NewDecoder(f).Decode(&val)
	if err != nil {
		return err
	}
	if info, ok := val["info"].(map[string]any); ok {
		if pieces, ok := info["pieces"].(string); ok {
			info["pieces"] = fmt.Sprintf("<<< %d bytes of data >>>", len(pieces))
		}
	}
	b, err := prettyjson.Marshal(val)
	if err != nil {
		return err
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

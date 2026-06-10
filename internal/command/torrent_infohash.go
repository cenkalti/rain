package command

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/urfave/cli"
	"github.com/zeebo/bencode"
)

func torrentInfoHashCommand() cli.Command {
	return cli.Command{
		Name:   "infohash",
		Usage:  "calculate and print info-hash in torrent file",
		Action: handleInfoHash,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "file,f",
				Required: true,
			},
		},
	}
}

func handleInfoHash(c *cli.Context) error {
	f, err := os.Open(c.String("file"))
	if err != nil {
		return err
	}
	defer f.Close()

	var metainfo struct {
		Info bencode.RawMessage `bencode:"info"`
	}
	err = bencode.NewDecoder(f).Decode(&metainfo)
	if err != nil {
		return err
	}
	sum := sha1.Sum(metainfo.Info)
	fmt.Println(hex.EncodeToString(sum[:]))
	return nil
}

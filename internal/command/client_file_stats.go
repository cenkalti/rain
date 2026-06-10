package command

import "github.com/urfave/cli"

func clientFileStatsCommand() cli.Command {
	return cli.Command{
		Name:     "file-stats",
		Usage:    "get stats of files in torrent",
		Category: "Getters",
		Action:   handleFileStats,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
		},
	}
}

func handleFileStats(c *cli.Context) error {
	resp, err := clt.GetTorrentFileStats(c.String("id"))
	if err != nil {
		return err
	}
	return printJSON(resp)
}

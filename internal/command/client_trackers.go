package command

import "github.com/urfave/cli"

func clientTrackersCommand() cli.Command {
	return cli.Command{
		Name:     "trackers",
		Usage:    "get trackers of torrent",
		Category: "Getters",
		Action:   handleTrackers,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
		},
	}
}

func handleTrackers(c *cli.Context) error {
	resp, err := clt.GetTorrentTrackers(c.String("id"))
	if err != nil {
		return err
	}
	return printJSON(resp)
}

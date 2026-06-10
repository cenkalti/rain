package command

import "github.com/urfave/cli"

func clientWebseedsCommand() cli.Command {
	return cli.Command{
		Name:     "webseeds",
		Usage:    "get webseed sources of torrent",
		Category: "Getters",
		Action:   handleWebseeds,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
		},
	}
}

func handleWebseeds(c *cli.Context) error {
	resp, err := clt.GetTorrentWebseeds(c.String("id"))
	if err != nil {
		return err
	}
	return printJSON(resp)
}

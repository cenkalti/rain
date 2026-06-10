package command

import "github.com/urfave/cli"

func clientFilesCommand() cli.Command {
	return cli.Command{
		Name:     "files",
		Usage:    "get file list of torrent",
		Category: "Getters",
		Action:   handleFiles,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
		},
	}
}

func handleFiles(c *cli.Context) error {
	resp, err := clt.GetTorrentFiles(c.String("id"))
	if err != nil {
		return err
	}
	return printJSON(resp)
}

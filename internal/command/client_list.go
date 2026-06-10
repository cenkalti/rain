package command

import "github.com/urfave/cli"

func clientListCommand() cli.Command {
	return cli.Command{
		Name:     "list",
		Usage:    "list torrents",
		Category: "Getters",
		Action:   handleList,
	}
}

func handleList(c *cli.Context) error {
	resp, err := clt.ListTorrents()
	if err != nil {
		return err
	}
	return printJSON(resp)
}

package command

import (
	"strings"

	"github.com/cenkalti/rain/v2/internal/console"
	"github.com/urfave/cli"
)

func clientConsoleCommand() cli.Command {
	return cli.Command{
		Name:     "console",
		Usage:    "show client console",
		Category: "Other",
		Action:   handleConsole,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "columns",
				Value:    "# ID Name",
				Required: false,
			},
		},
	}
}

func handleConsole(c *cli.Context) error {
	columns := strings.Split(c.String("columns"), " ")

	con := console.New(clt, columns)
	return con.Run()
}

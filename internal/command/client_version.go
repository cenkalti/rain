package command

import (
	"os"

	"github.com/urfave/cli"
)

func clientVersionCommand() cli.Command {
	return cli.Command{
		Name:     "version",
		Usage:    "server version",
		Category: "Getters",
		Action:   handleVersion,
	}
}

func handleVersion(c *cli.Context) error {
	version, err := clt.ServerVersion()
	if err != nil {
		return err
	}
	_, _ = os.Stdout.WriteString(version)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

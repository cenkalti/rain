package command

import "github.com/urfave/cli"

func clientCleanDatabaseCommand() cli.Command {
	return cli.Command{
		Name:     "clean-database",
		Usage:    "clean session database",
		Category: "Actions",
		Action:   handleCleanDatabase,
	}
}

func handleCleanDatabase(c *cli.Context) error {
	return clt.CleanDatabase()
}

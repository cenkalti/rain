package command

import "github.com/urfave/cli"

func TorrentCommand() cli.Command {
	return cli.Command{
		Name:  "torrent",
		Usage: "manage torrent files",
		Subcommands: []cli.Command{
			torrentShowCommand(),
			torrentInfoHashCommand(),
			torrentCreateCommand(),
		},
	}
}

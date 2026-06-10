package command

import (
	"os"

	"github.com/cenkalti/rain/v2/torrent"
	"github.com/urfave/cli"
)

func CompactDatabaseCommand() cli.Command {
	return cli.Command{
		Name:   "compact-database",
		Usage:  "rewrite database to save up space",
		Action: handleCompactDatabase,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "config,c",
				Usage: "read config from `FILE`",
				Value: "$HOME/rain/config.yaml",
			},
		},
	}
}

func handleCompactDatabase(c *cli.Context) error {
	cfg, err := prepareConfig(c)
	if err != nil {
		return err
	}
	cfg.ResumeOnStartup = false
	cfg.RPCEnabled = false
	cfg.DHTEnabled = false
	ses, err := torrent.NewSession(cfg)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "rain-compact-database-")
	if err != nil {
		return err
	}
	err = f.Close()
	if err != nil {
		return err
	}
	err = ses.CompactDatabase(f.Name())
	if err != nil {
		return err
	}
	dbPath := os.ExpandEnv(cfg.Database)
	err = os.Rename(dbPath, dbPath+".bak")
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), dbPath)
}

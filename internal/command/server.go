package command

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/cenkalti/rain/v2/torrent"
	"github.com/urfave/cli"
)

func ServerCommand() cli.Command {
	return cli.Command{
		Name:  "server",
		Usage: "run rpc server and torrent client",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "config,c",
				Usage: "read config from `FILE`",
				Value: "$HOME/rain/config.yaml",
			},
		},
		Action: handleServer,
	}
}

func handleServer(c *cli.Context) error {
	cfg, err := prepareConfig(c)
	if err != nil {
		return err
	}
	ses, err := torrent.NewSession(cfg)
	if err != nil {
		return err
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	s := <-ch
	log.Noticef("received %s, stopping server", s)

	return ses.Close()
}

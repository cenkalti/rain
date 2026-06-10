package command

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cenkalti/rain/v2/torrent"
	"github.com/urfave/cli"
)

func MagnetToTorrentCommand() cli.Command {
	return cli.Command{
		Name:  "magnet-to-torrent",
		Usage: "download torrent from magnet link",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "config,c",
				Usage: "read config from `FILE`",
				Value: "$HOME/rain/config.yaml",
			},
			cli.StringFlag{
				Name:     "magnet,m",
				Usage:    "magnet link",
				Required: true,
			},
			cli.StringFlag{
				Name:  "output,o",
				Usage: "output file",
			},
			cli.DurationFlag{
				Name:  "timeout,t",
				Usage: "command fails if torrent cannot be downloaded after duration",
				Value: time.Minute,
			},
		},
		Action: handleMagnetToTorrent,
	}
}

func handleMagnetToTorrent(c *cli.Context) error {
	arg := c.String("magnet")
	output := c.String("output")
	timeout := c.Duration("timeout")
	cfg, err := prepareConfig(c)
	if err != nil {
		return err
	}
	dbFile, err := os.CreateTemp("", "")
	if err != nil {
		return err
	}
	dbFileName := dbFile.Name()
	defer os.Remove(dbFileName)
	err = dbFile.Close()
	if err != nil {
		return err
	}
	cfg.Database = dbFileName
	ses, err := torrent.NewSession(cfg)
	if err != nil {
		return err
	}
	defer ses.Close()
	opt := &torrent.AddTorrentOptions{
		StopAfterMetadata: true,
	}
	t, err := ses.AddURI(arg, opt)
	if err != nil {
		return err
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	timeoutC := time.After(timeout)
	metadataC := t.NotifyMetadata()
	for {
		select {
		case s := <-ch:
			log.Noticef("received %s, stopping torrent", s)
			err = t.Stop()
			if err != nil {
				return err
			}
		case <-time.After(timeout):
			stats := t.Stats()
			log.Infof("Status: %s, Peers: %d\n", stats.Status.String(), stats.Peers.Total)
		case <-metadataC:
			name := output
			if name == "" {
				name = t.Name() + ".torrent"
			}
			data, err := t.Torrent()
			if err != nil {
				return err
			}
			f, err := os.Create(name)
			if err != nil {
				return err
			}
			_, err = f.Write(data)
			if err != nil {
				return err
			}
			err = f.Close()
			if err != nil {
				return err
			}
			fmt.Println(name)
			return nil
		case <-timeoutC:
			return fmt.Errorf("metadata cannot be downloaded in %s, try increasing timeout", timeout.String())
		case err = <-t.NotifyStop():
			return err
		}
	}
}

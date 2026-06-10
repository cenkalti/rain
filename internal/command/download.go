package command

import (
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cenkalti/rain/v2/internal/magnet"
	"github.com/cenkalti/rain/v2/internal/metainfo"
	"github.com/cenkalti/rain/v2/torrent"
	"github.com/urfave/cli"
)

func DownloadCommand() cli.Command {
	return cli.Command{
		Name:  "download",
		Usage: "download single torrent",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "config,c",
				Usage: "read config from `FILE`",
				Value: "$HOME/rain/config.yaml",
			},
			cli.StringFlag{
				Name:     "torrent,t",
				Usage:    "torrent file or URI",
				Required: true,
			},
			cli.BoolFlag{
				Name:  "seed,s",
				Usage: "continue seeding after download is finished",
			},
			cli.StringFlag{
				Name:  "resume,r",
				Usage: "path to .resume file",
			},
		},
		Action: handleDownload,
	}
}

func handleDownload(c *cli.Context) error {
	arg := c.String("torrent")
	seed := c.Bool("seed")
	resume := c.String("resume")
	cfg, err := prepareConfig(c)
	if err != nil {
		return err
	}
	cfg.DataDir = "."
	cfg.DataDirIncludesTorrentID = false
	var ih torrent.InfoHash
	if strings.HasPrefix(arg, "magnet:") {
		magnet, err := magnet.New(arg)
		if err != nil {
			return err
		}
		ih = torrent.InfoHash(magnet.InfoHash)
		cfg.Database = magnet.Name + ".resume"
	} else {
		var rc io.ReadCloser

		if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			resp, err := http.DefaultClient.Get(arg) // nolint: noctx
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			rc = resp.Body
		} else {
			f, err := os.Open(arg)
			if err != nil {
				return err
			}
			defer f.Close()
			rc = f
		}
		mi, err := metainfo.New(rc)
		if err != nil {
			return err
		}
		rc.Close()
		ih = mi.Info.Hash
		cfg.Database = mi.Info.Name + ".resume"
	}
	if resume != "" {
		cfg.Database = resume
	}
	ses, err := torrent.NewSession(cfg)
	if err != nil {
		return err
	}
	defer ses.Close()
	var t *torrent.Torrent
	torrents := ses.ListTorrents()
	if len(torrents) > 0 && torrents[0].InfoHash() == ih {
		// Resume data exists
		t = torrents[0]
		err = t.Start()
	} else {
		// Add as new torrent
		opt := &torrent.AddTorrentOptions{
			StopAfterDownload: !seed,
		}
		if isURI(arg) {
			t, err = ses.AddURI(arg, opt)
		} else {
			var f *os.File
			f, err = os.Open(arg)
			if err != nil {
				return err
			}
			t, err = ses.AddTorrent(f, opt)
			f.Close()
		}
	}
	if err != nil {
		return err
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	for {
		select {
		case s := <-ch:
			log.Noticef("received %s, stopping torrent", s)
			err = t.Stop()
			if err != nil {
				return err
			}
		case <-time.After(time.Second):
			stats := t.Stats()
			progress := 0
			if stats.Bytes.Total > 0 {
				progress = int((stats.Bytes.Completed * 100) / stats.Bytes.Total)
			}
			eta := "?"
			if stats.ETA != nil {
				eta = stats.ETA.String()
			}
			log.Infof("Status: %s, Progress: %d%%, Peers: %d, Speed: %dK/s, ETA: %s\n", stats.Status.String(), progress, stats.Peers.Total, stats.Speed.Download/1024, eta)
		case err = <-t.NotifyStop():
			return err
		}
	}
}

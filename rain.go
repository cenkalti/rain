package main

import (
	"errors"
	"os"
	"os/signal"
	"runtime/pprof"
	"strings"
	"syscall"

	"github.com/cenkalti/log"
	"github.com/cenkalti/rain/client"
	"github.com/cenkalti/rain/internal/clientversion"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/resume/torrentresume"
	"github.com/cenkalti/rain/storage/filestorage"
	"github.com/cenkalti/rain/torrent"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli"
)

var (
	cfg = client.NewConfig()
	app = cli.NewApp()
)

func main() {
	app.Version = clientversion.Version
	app.Usage = "BitTorrent client"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "config, c",
			Usage: "read config from `FILE`",
		},
		cli.StringFlag{
			Name:  "cpuprofile",
			Usage: "write cpu profile to `FILE`",
		},
		cli.BoolFlag{
			Name:  "debug, d",
			Usage: "enable debug log",
		},
	}
	app.Before = handleBeforeCommand
	app.After = handleAfterCommand
	app.Commands = []cli.Command{
		{
			Name:      "download",
			Usage:     "download torrent or magnet",
			ArgsUsage: "[torrent path or magnet link]",
			Action:    handleDownload,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "dest",
					Usage: "save files under `DIR`",
					Value: ".",
				},
				cli.IntFlag{
					Name:  "port",
					Usage: "peer listen port",
				},
				cli.BoolFlag{
					Name:  "seed",
					Usage: "continue seeding after download finishes",
				},
			},
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func handleBeforeCommand(c *cli.Context) error {
	cpuprofile := c.GlobalString("cpuprofile")
	if cpuprofile != "" {
		f, err := os.Create(cpuprofile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
	}
	if c.GlobalBool("debug") {
		logger.SetLogLevel(log.DEBUG)
	}
	configPath := c.GlobalString("config")
	if configPath != "" {
		cp, err := homedir.Expand(configPath)
		if err != nil {
			log.Fatal(err)
		}
		err = cfg.LoadFile(cp)
		if err != nil {
			log.Fatal(err)
		}
	}
	return nil
}

func handleAfterCommand(c *cli.Context) error {
	if c.GlobalString("cpuprofile") != "" {
		defer pprof.StopCPUProfile()
	}
	return nil
}

func handleDownload(c *cli.Context) error {
	path := c.Args().Get(0)
	if path == "" {
		return errors.New("first argument must be a torrent file or magnet link")
	}
	sto, err := filestorage.New(c.String("dest"))
	if err != nil {
		log.Fatal(err)
	}
	res, err := torrentresume.New("rain.resume")
	if err != nil {
		log.Fatal(err)
	}
	var t *torrent.Torrent
	if strings.HasPrefix(path, "magnet:") {
		t, err = torrent.DownloadMagnet(path, c.Int("port"), sto, res)
	} else {
		f, err2 := os.Open(path) // nolint: gosec
		if err2 != nil {
			log.Fatal(err2)
		}
		t, err = torrent.DownloadTorrent(f, c.Int("port"), sto, res)
		_ = f.Close()
	}
	if err != nil {
		log.Fatal(err)
	}

	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)
LOOP:
	for {
		select {
		case <-sigC:
			break LOOP
		case <-t.NotifyComplete():
			if !c.Bool("seed") {
				break LOOP
			}
		case err = <-t.NotifyError():
			log.Error(err)
			os.Exit(1)
		}
	}
	t.Close()
	return nil
}

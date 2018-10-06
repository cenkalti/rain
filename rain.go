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
	"github.com/cenkalti/rain/resume/torrentresume"
	"github.com/cenkalti/rain/storage/filestorage"
	"github.com/cenkalti/rain/torrent"
	"github.com/cenkalti/rain/torrent/internal/clientversion"
	"github.com/cenkalti/rain/torrent/internal/logger"
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
		pprof.StopCPUProfile()
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
	var t *torrent.Torrent
	if strings.HasPrefix(path, "magnet:") {
		t, err = torrent.NewMagnet(path, c.Int("port"), sto)
	} else {
		f, err2 := os.Open(path) // nolint: gosec
		if err2 != nil {
			log.Fatal(err2)
		}
		t, err = torrent.New(f, c.Int("port"), sto)
		_ = f.Close()
	}
	if err != nil {
		log.Fatal(err)
	}
	defer t.Close()

	res, err := torrentresume.New(t.Name() + "." + t.InfoHash() + ".resume")
	if err != nil {
		log.Fatal(err)
	}
	err = t.SetResume(res)
	if err != nil {
		log.Fatal(err)
	}

	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)

	t.Start()
	select {
	case <-t.NotifyComplete():
		if !c.Bool("seed") {
			return nil
		}
		select {
		case <-sigC:
		case err = <-t.NotifyError():
			return err
		}
	case <-sigC:
	case err = <-t.NotifyError():
		return err
	}
	return nil
}

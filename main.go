package main

import (
	"net/http"
	_ "net/http/pprof" // registers pprof handlers on the default mux
	"os"
	"runtime"
	"runtime/pprof"

	"github.com/cenkalti/rain/v2/internal/command"
	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/cenkalti/rain/v2/torrent"
	"github.com/urfave/cli"
)

var log = logger.New("rain")

func main() {
	app := cli.NewApp()
	app.Version = torrent.Version
	app.Usage = "BitTorrent client from https://put.io"
	app.EnableBashCompletion = true
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "debug,d",
			Usage: "enable debug log",
		},
		cli.StringFlag{
			Name:   "cpuprofile",
			Hidden: true,
			Usage:  "write cpu profile to `FILE`",
		},
		cli.StringFlag{
			Name:   "memprofile",
			Hidden: true,
			Usage:  "write memory profile to `FILE`",
		},
		cli.IntFlag{
			Name:   "blockprofile",
			Hidden: true,
			Usage:  "enable blocking profiler",
		},
		cli.StringFlag{
			Name:   "pprof",
			Hidden: true,
			Usage:  "run pprof server on `ADDR`",
		},
	}
	app.Before = handleBeforeCommand
	app.After = handleAfterCommand
	app.Commands = []cli.Command{
		command.BashAutocompleteCommand(),
		command.DownloadCommand(),
		command.MagnetToTorrentCommand(),
		command.ServerCommand(),
		command.ClientCommand(),
		command.BoltBrowserCommand(),
		command.CompactDatabaseCommand(),
		command.TorrentCommand(),
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
	pprofFlag := c.GlobalString("pprof")
	if pprofFlag != "" {
		go func() {
			log.Notice(http.ListenAndServe(pprofFlag, nil))
		}()
	}
	blockProfile := c.GlobalInt("blockprofile")
	if blockProfile != 0 {
		runtime.SetBlockProfileRate(blockProfile)
	}
	if c.GlobalBool("debug") {
		logger.SetDebug()
	}
	return nil
}

func handleAfterCommand(c *cli.Context) error {
	if c.GlobalString("cpuprofile") != "" {
		pprof.StopCPUProfile()
	}
	memprofile := c.GlobalString("memprofile")
	if memprofile != "" {
		f, err := os.Create(memprofile)
		if err != nil {
			log.Fatal(err)
		}
		err = pprof.WriteHeapProfile(f)
		if err != nil {
			log.Fatal(err)
		}
		err = f.Close()
		if err != nil {
			log.Fatal(err)
		}
	}
	return nil
}

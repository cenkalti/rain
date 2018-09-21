package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"

	"github.com/cenkalti/log"
	"github.com/cenkalti/rain/client"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/resume/torrentresume"
	"github.com/cenkalti/rain/torrent"
	"github.com/mitchellh/go-homedir"
)

var (
	configPath = flag.String("config", "", "config path")
	dest       = flag.String("dest", ".", "where to download")
	port       = flag.Int("port", 0, "listen port")
	debug      = flag.Bool("debug", false, "enable debug log")
	version    = flag.Bool("version", false, "version")
	seed       = flag.Bool("seed", false, "continue seeding after dowload finishes")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to `file`")
)

func main() {
	flag.Parse()
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}
	if *version {
		fmt.Println(torrent.Version)
		return
	}
	args := flag.Args()
	if len(args) == 0 {
		_, _ = fmt.Fprintln(os.Stderr, "Give a torrent file as first argument!")
		os.Exit(1)
	}
	if *debug {
		logger.SetLogLevel(log.DEBUG)
	}
	cfg := client.NewConfig()
	if *configPath != "" {
		cp, err := homedir.Expand(*configPath)
		if err != nil {
			log.Fatal(err)
		}
		err = cfg.LoadFile(cp)
		if err != nil {
			log.Fatal(err)
		}
	}
	f, err := os.Open(args[0])
	if err != nil {
		log.Fatal(err)
	}
	res, err := torrentresume.New("rain.resume")
	if err != nil {
		log.Fatal(err)
	}
	t, err := torrent.New(f, *dest, *port, res)
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
			if !*seed {
				break LOOP
			}
		case err = <-t.NotifyError():
			log.Error(err)
			os.Exit(1)
		}
	}
	err = t.Close()
	if err != nil {
		log.Fatal(err)
	}
}

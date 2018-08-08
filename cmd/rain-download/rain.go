package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/cenkalti/log"
	"github.com/mitchellh/go-homedir"

	"github.com/cenkalti/rain"
	"github.com/cenkalti/rain/logger"
)

var (
	configPath = flag.String("config", "", "config path")
	dest       = flag.String("dest", ".", "where to download")
	debug      = flag.Bool("debug", false, "enable debug log")
	version    = flag.Bool("version", false, "version")
	seed       = flag.Bool("seed", false, "continue seeding after dowload finishes")
)

func main() {
	flag.Parse()

	if *version {
		fmt.Println(rain.Version)
		return
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Give a torrent file as first argument!")
		os.Exit(1)
	}

	rand.Seed(time.Now().UnixNano())

	if *debug {
		logger.SetLogLevel(log.DEBUG)
	}

	c := rain.NewConfig()
	if *configPath != "" {
		cp, err := homedir.Expand(*configPath)
		if err != nil {
			log.Fatal(err)
		}
		err = c.LoadFile(cp)
		if err != nil {
			log.Fatal(err)
		}
	}

	r, err := rain.NewClient(c)
	if err != nil {
		log.Fatal(err)
	}

	err = r.Listen()
	if err != nil {
		log.Fatal(err)
	}

	f, err := os.Open(args[0])
	if err != nil {
		log.Fatal(err)
	}

	t, err := r.AddTorrent(f, *dest)
	if err != nil {
		log.Fatal(err)
	}

	t.Start()

	if *seed {
		select {}
	} else {
		<-t.CompleteNotify()
	}
}

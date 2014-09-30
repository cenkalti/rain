package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/cenkalti/log"
	"github.com/mitchellh/go-homedir"

	"github.com/cenkalti/rain"
)

const defaultConfig = "~/.rain.yaml"

var (
	config  = flag.String("c", defaultConfig, "config file")
	where   = flag.String("w", rain.DefaultConfig.DownloadDir, "where to download")
	port    = flag.Int("p", int(rain.DefaultConfig.Port), "listen port for incoming peer connections")
	debug   = flag.Bool("d", false, "enable debug log")
	version = flag.Bool("v", false, "version")
)

func main() {
	flag.Parse()

	if *version == true {
		fmt.Println(rain.Build)
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Give a torrent file as first argument!")
		os.Exit(1)
	}

	rand.Seed(time.Now().UnixNano())

	if *debug {
		rain.SetLogLevel(log.DEBUG)
	}

	var configFile string
	if *config != "" {
		configFile = *config
	} else {
		configFile = defaultConfig
	}

	var err error
	configFile, err = homedir.Expand(configFile)
	if err != nil {
		fmt.Fprint(os.Stderr, "Cannot determine home directory! Specify config file with -c flag.")
		os.Exit(1)
	}

	c, err := rain.LoadConfig(configFile)
	if err != nil {
		log.Fatal(err)
	}

	if *port > math.MaxUint16 {
		log.Fatal("invalid port number")
	}
	c.Port = uint16(*port)

	if *where != rain.DefaultConfig.DownloadDir {
		c.DownloadDir = *where
	}

	r, err := rain.New(c)
	if err != nil {
		log.Fatal(err)
	}

	err = r.Listen()
	if err != nil {
		log.Fatal(err)
	}

	t, err := r.Add(args[0], "")
	if err != nil {
		log.Fatal(err)
	}

	r.Start(t)
	<-t.Finished()
}

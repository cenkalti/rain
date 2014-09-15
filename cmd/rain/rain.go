package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/cenkalti/log"
	"github.com/cenkalti/rain"
	"github.com/mitchellh/go-homedir"
)

const defaultConfig = "~/.rain.yaml"

var (
	config = flag.String("c", defaultConfig, "config file")
	where  = flag.String("w", ".", "where to download")
	port   = flag.Int("p", rain.DefaultConfig.Port, "listen port for incoming peer connections")
	debug  = flag.Bool("d", false, "enable debug log")
)

func main() {
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, "Give a torrent file as first argument!")
		os.Exit(1)
	}

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

	c.Port = *port

	r, err := rain.New(c)
	if err != nil {
		log.Fatal(err)
	}

	err = r.Listen()
	if err != nil {
		log.Fatal(err)
	}

	if err = r.Download(args[0], *where); err != nil {
		log.Fatal(err)
	}
}

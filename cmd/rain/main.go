package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/cenkalti/log"
	"github.com/cenkalti/rain"
)

var (
	where = flag.String("w", ".", "where to download")
	port  = flag.Int("p", 0, "listen port for incoming peer connections")
	debug = flag.Bool("d", false, "enable debug log")
)

func main() {
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, "give a torrent file")
		os.Exit(1)
	}

	if *debug {
		rain.SetLogLevel(log.DEBUG)
	}

	r, err := rain.New(*port)
	if err != nil {
		log.Fatal(err)
	}

	if err = r.Download(args[0], *where); err != nil {
		log.Fatal(err)
	}
}

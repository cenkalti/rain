package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/cenkalti/log"
	"github.com/cenkalti/rain/rain"
)

var (
	where = flag.String("w", ".", "where to download")
	port  = flag.Int("p", 0, "listen port")
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
		log.SetLevel(log.DEBUG)
	}

	r, err := rain.New()
	if err != nil {
		log.Fatal(err)
	}

	if err = r.ListenPeerPort(*port); err != nil {
		log.Fatal(err)
	}

	if err := r.Download(args[0], *where); err != nil {
		log.Fatal(err)
	}
}

package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/cenkalti/rain/rain"
)

var (
	where = flag.String("w", ".", "where to download")
	port  = flag.Int("p", 0, "listen port")
)

func main() {
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, "give a torrent file")
		os.Exit(1)
	}

	r, err := rain.New()
	if err != nil {
		log.Fatal(err)
	}

	err = r.ListenPeerPort(*port)
	if err != nil {
		log.Fatal(err)
	}

	if err := r.Download(args[0], *where); err != nil {
		log.Fatal(err)
	}
}

package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/cenkalti/rain/rain"
)

var where = flag.String("w", ".", "where to download")
var port = flag.Int("p", rain.DefaultPeerPort, "listen port")

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

	r.Port = *port
	err = r.ListenPeerPort()
	if err != nil {
		log.Fatal(err)
	}

	if err := r.Download(args[0], *where); err != nil {
		log.Fatal(err)
	}
}

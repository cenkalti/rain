package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/cenkalti/rain/rain"
)

func main() {
	where := flag.String("w", ".", "where to download")

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

	if err := r.Download(args[0], *where); err != nil {
		log.Fatal(err)
	}
}

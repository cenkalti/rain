package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/cenkalti/rain/rain"
)

func main() {
	r := rain.New()

	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, "give a torrent file")
		os.Exit(1)
	}

	err := r.Download(args[0])
	if err != nil {
		log.Fatal(err)
	}
}

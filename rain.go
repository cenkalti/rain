package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, "give a torrent file")
		os.Exit(1)
	}
	mi := new(TorrentFile)
	err := mi.Load(args[0])
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("--- mi: %#v\n", mi)
}

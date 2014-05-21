package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"
)

func main() {
	var err error

	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, "give a torrent file")
		os.Exit(1)
	}

	rand.Seed(time.Now().UnixNano())

	mi := new(TorrentFile)
	if err = mi.Load(args[0]); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("--- mi: %#v\n", mi)

	download := &Download{
		TorrentFile: mi,
	}

	tracker, err := NewTracker(mi.Announce)
	if err != nil {
		log.Fatal(err)
	}

	_, err = tracker.Connect()
	if err != nil {
		log.Fatal(err)
	}

	ann, err := tracker.Announce(download)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("--- ann: %#v\n", ann)
}

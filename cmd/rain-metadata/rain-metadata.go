package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"time"

	"github.com/cenkalti/log"

	"github.com/cenkalti/rain"
	"github.com/cenkalti/rain/internal/tracker"
)

var (
	debug   = flag.Bool("d", false, "enable debug log")
	timeout = flag.Uint("t", 5000, "tracker timeout (ms)")
)

func main() {
	flag.Parse()

	if *debug {
		rain.SetLogLevel(log.DEBUG)
	}

	magnet, err := rain.ParseMagnet(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	tracker.HTTPTimeout = time.Duration(*timeout) * time.Millisecond

	d, err := rain.NewMetadataDownloader(magnet)
	if err != nil {
		log.Fatal(err)
	}

	go d.Run()

	m := <-d.Result

	b, err := json.Marshal(m)
	if err != nil {
		log.Fatal(err)
	}

	_, err = fmt.Print(string(b))
	if err != nil {
		log.Fatal(err)
	}
}

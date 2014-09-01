package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/cenkalti/log"

	"github.com/cenkalti/rain"
	"github.com/cenkalti/rain/internal/magnet"
	"github.com/cenkalti/rain/internal/tracker"
)

var (
	debug   = flag.Bool("d", false, "enable debug log")
	timeout = flag.Uint("t", 5000, "tracker timeout (ms)")
)

func main() {
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, "rain-metadata: Download metadata from magnet link.\nUsage: rain-metadata [options] magnet\nOptions:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if *debug {
		rain.SetLogLevel(log.DEBUG)
	}

	magnet, err := magnet.Parse(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	// Be more aggressive than normal.
	tracker.HTTPTimeout = time.Duration(*timeout) * time.Millisecond
	tracker.UDPBackOff = func() backoff.BackOff { return new(udpBackOff) }

	d, err := rain.NewMetadataDownloader(magnet)
	if err != nil {
		log.Fatal(err)
	}

	go d.Run()

	m := <-d.Result

	err = json.NewEncoder(os.Stdout).Encode(m)
	if err != nil {
		log.Fatal(err)
	}
}

type udpBackOff struct{}

func (b *udpBackOff) NextBackOff() time.Duration {
	return time.Duration(*timeout) * time.Millisecond
}

func (b *udpBackOff) Reset() {}

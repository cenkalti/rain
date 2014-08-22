package main

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/cenkalti/log"

	"github.com/cenkalti/rain"
)

var (
	debug = flag.Bool("d", false, "enable debug log")
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

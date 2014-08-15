package main

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/cenkalti/log"

	"github.com/cenkalti/rain"
	"github.com/cenkalti/rain/internal/protocol"
)

var (
	debug    = flag.Bool("d", false, "enable debug log")
	infoHash = flag.String("i", "", "info hash")
)

func main() {
	flag.Parse()

	if *debug {
		rain.SetLogLevel(log.DEBUG)
	}

	if len(*infoHash) != 0 {
		ih, err := protocol.NewInfoHashString(*infoHash)
		if err != nil {
			log.Fatal(err)
		}

		m, err := rain.Metadata(ih)
		if err != nil {
			log.Fatal(err)
		}

		b, err := json.Marshal(m)
		if err != nil {
			log.Fatal(err)
		}

		_, err = fmt.Print(string(b))
		if err != nil {
			log.Fatal(err)
		}
	}
}

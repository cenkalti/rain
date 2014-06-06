package rain

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
)

type ConnectRequest struct {
	TrackerRequestHeader
}

type ConnectResponse struct {
	TrackerMessageHeader
	ConnectionID int64
}

// connect sends a ConnectRequest and returns a ConnectionID given by the tracker.
// On error, it backs off with the algorithm described in BEP15 and retries.
// It does not return until tracker sends a ConnectionID.
func (t *Tracker) connect() int64 {
	req := new(ConnectRequest)
	req.SetConnectionID(ConnectionIDMagic)
	req.SetAction(Connect)

	write := func(req TrackerRequest) {
		binary.Write(t.conn, binary.BigEndian, req)
	}

	// TODO wait before retry
	for {
		data, err := t.retry(req, write, nil)
		if err != nil {
			log.Println(err)
			continue
		}

		var response ConnectResponse
		err = binary.Read(bytes.NewReader(data), binary.BigEndian, &response)
		if err != nil {
			log.Println(err)
			continue
		}

		if response.Action != Connect {
			log.Println("invalid action in connect response")
			continue
		}

		fmt.Printf("--- connect Response: %#v\n", response)
		return response.ConnectionID
	}
}

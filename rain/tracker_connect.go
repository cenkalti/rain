package rain

import (
	"bytes"
	"encoding/binary"

	"github.com/cenkalti/log"
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
	req.SetConnectionID(connectionIDMagic)
	req.SetAction(Connect)

	write := func(req TrackerRequest) {
		binary.Write(t.conn, binary.BigEndian, req)
	}

	// TODO wait before retry
	for {
		data, err := t.retry(req, write, nil)
		if err != nil {
			log.Error(err)
			continue
		}

		var response ConnectResponse
		err = binary.Read(bytes.NewReader(data), binary.BigEndian, &response)
		if err != nil {
			log.Error(err)
			continue
		}

		if response.Action != Connect {
			log.Error("invalid action in connect response")
			continue
		}

		log.Debugf("connect Response: %#v\n", response)
		return response.ConnectionID
	}
}

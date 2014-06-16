package rain

import (
	"bytes"
	"encoding/binary"
)

type connectRequest struct {
	trackerRequestHeader
}

type connectResponse struct {
	trackerMessageHeader
	ConnectionID int64
}

// connect sends a connectRequest and returns a ConnectionID given by the tracker.
// On error, it backs off with the algorithm described in BEP15 and retries.
// It does not return until tracker sends a ConnectionID.
func (t *tracker) connect() int64 {
	req := new(connectRequest)
	req.SetConnectionID(connectionIDMagic)
	req.SetAction(Connect)

	write := func(req trackerRequest) {
		binary.Write(t.conn, binary.BigEndian, req)
	}

	// TODO wait before retry
	for {
		data, err := t.retry(req, write, nil)
		if err != nil {
			t.log.Error(err)
			continue
		}

		var response connectResponse
		err = binary.Read(bytes.NewReader(data), binary.BigEndian, &response)
		if err != nil {
			t.log.Error(err)
			continue
		}

		if response.Action != Connect {
			t.log.Error("invalid action in connect response")
			continue
		}

		t.log.Debugf("connect Response: %#v\n", response)
		return response.ConnectionID
	}
}

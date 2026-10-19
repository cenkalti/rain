package udptracker

import (
	"net"
	"time"
)

type connection struct {
	*requestBase
	*connectRequest

	// holds the announce requests that needs to be sent after the connection is successful.
	requests []*transportRequest

	// These fields are set by Transport.Run loop if connected successfully.
	addr        *net.UDPAddr
	id          int64
	connectedAt time.Time
}

var _ udpRequest = (*connection)(nil)

func newConnection(req *transportRequest) *connection {
	return &connection{
		requestBase:    newRequestBase(req.ctx, req.dest),
		connectRequest: newConnectRequest(),
		requests:       []*transportRequest{req},
	}
}

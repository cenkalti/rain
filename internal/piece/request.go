package piece

import (
	"time"

	"github.com/cenkalti/rain/internal/bitfield"
)

type Request struct {
	createdAt        time.Time
	BlocksRequesting *bitfield.Bitfield
	BlocksRequested  *bitfield.Bitfield
	BlocksReceiving  *bitfield.Bitfield
	BlocksReceived   *bitfield.Bitfield
	Data             []byte // buffer for received blocks
}

func (r *Request) ResetWaitingRequests() {
	r.BlocksRequesting.ClearAll()
	r.BlocksRequested.ClearAll()
	copy(r.BlocksRequesting.Bytes(), r.BlocksReceiving.Bytes())
	copy(r.BlocksRequested.Bytes(), r.BlocksReceiving.Bytes())
}

func (r *Request) Outstanding() uint32 {
	o := int64(r.BlocksRequested.Count()) - int64(r.BlocksReceiving.Count())
	if o < 0 {
		o = 0
	}
	return uint32(o)
}

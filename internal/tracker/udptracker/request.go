package udptracker

import (
	"context"
	"encoding/binary"

	"github.com/cenkalti/rain/internal/tracker"
)

type transportRequest struct {
	*requestBase
	transferAnnounceRequest
}

var _ udpRequest = (*transportRequest)(nil)

func newTransportRequest(ctx context.Context, req tracker.AnnounceRequest, dest string, urlData string) *transportRequest {
	request := &announceRequest{
		InfoHash:   req.Torrent.InfoHash,
		PeerID:     req.Torrent.PeerID,
		Downloaded: req.Torrent.BytesDownloaded,
		Left:       req.Torrent.BytesLeft,
		Uploaded:   req.Torrent.BytesUploaded,
		Event:      req.Event,
		NumWant:    int32(req.NumWant),
		Port:       uint16(req.Torrent.Port),
	}
	binary.BigEndian.PutUint32(request.PeerID[16:20], request.Key)
	request.Action = actionAnnounce

	return &transportRequest{
		requestBase: newRequestBase(ctx, dest),
		transferAnnounceRequest: transferAnnounceRequest{
			announceRequest: request,
			urlData:         urlData,
		},
	}
}

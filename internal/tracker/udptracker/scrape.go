package udptracker

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// udpScrapeRequest is the request sent to the tracker for scrape.
type udpScrapeRequest struct {
	InfoHash [20]byte
}

// MarshalBinary encodes the scrape request into binary format.
func (r *udpScrapeRequest) MarshalBinary() ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 20))
	_, err := buf.Write(r.InfoHash[:])
	return buf.Bytes(), err
}

// UnmarshalBinary decodes the scrape request from binary format.
func (r *udpScrapeRequest) UnmarshalBinary(data []byte) error {
	if len(data) < 20 {
		return errors.New("invalid scrape request")
	}
	copy(r.InfoHash[:], data[:20])
	return nil
}

// udpScrapeResponse is the response received from the tracker for scrape.
type udpScrapeResponse struct {
	Complete   int32
	Downloaded int32
	Incomplete int32
}

// parseScrapeResponse parses the scrape response from the tracker.
func (t *UDPTracker) parseScrapeResponse(data []byte) (*udpScrapeResponse, error) {
	if len(data) < 12 {
		return nil, errors.New("invalid scrape response")
	}
	
	var response udpScrapeResponse
	err := binary.Read(bytes.NewReader(data), binary.BigEndian, &response)
	if err != nil {
		return nil, err
	}
	
	return &response, nil
}
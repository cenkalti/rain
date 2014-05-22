package main

// http://www.rasterbar.com/products/libtorrent/udp_tracker_protocol.html
// http://xbtt.sourceforge.net/udp_tracker_protocol.html

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"time"
)

const NumWant = 50

type Action int32
type Event int32

// Actions
const (
	Connect Action = iota
	Announce
	Scrape
	Error
)

// Events
const (
	None Event = iota
	Completed
	Started
	Stopped
)

type Tracker struct {
	URL *url.URL
	// ConnectionID given by the tracker. Set after connect.
	ConnectionID int64
	conn         *net.UDPConn
	buf          []byte
}

func NewTracker(trackerURL string) (*Tracker, error) {
	parsed, err := url.Parse(trackerURL)
	if err != nil {
		return nil, err
	}
	return &Tracker{
		URL: parsed,
		buf: make([]byte, 512),
	}, nil
}

type ConnectRequest struct {
	ConnectionID int64
	TrackerMessageHeader
}

type ConnectResponse struct {
	TrackerMessageHeader
	ConnectionID int64
}

func (t *Tracker) Connect() (*ConnectResponse, error) {
	serverAddr, err := net.ResolveUDPAddr("udp", t.URL.Host)
	if err != nil {
		return nil, err
	}
	t.conn, err = net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		return nil, err
	}

	var response ConnectResponse
	var request = ConnectRequest{
		ConnectionID: 0x41727101980,
		TrackerMessageHeader: TrackerMessageHeader{
			Action:        Connect,
			TransactionID: rand.Int31(),
		},
	}

	_, err = t.request(&request, &response)
	if err != nil {
		return nil, err
	}
	if response.Action != Connect {
		return nil, errors.New("invalid action")
	}

	t.ConnectionID = response.ConnectionID
	fmt.Printf("--- Response: %#v\n", response)
	return &response, nil
}

type AnnounceRequest struct {
	ConnectionID int64
	TrackerMessageHeader
	InfoHash   [20]byte
	PeerID     [20]byte
	Downloaded int64
	Left       int64
	Uploaded   int64
	Event      Event
	IP         uint32
	Key        uint32
	NumWant    int32
	Port       uint16
	Extensions uint16
}

type announceResponse struct {
	TrackerMessageHeader
	Interval int32
	Leechers int32
	Seeders  int32
}

type AnnounceResponse struct {
	announceResponse
	Peers []Peer
}

type Peer struct {
	IP   int32
	Port uint16
}

func (p Peer) Addr() net.Addr {
	return nil
}

func (t *Tracker) Announce(d *Download) (*AnnounceResponse, error) {
	request := &AnnounceRequest{
		ConnectionID: t.ConnectionID,
		TrackerMessageHeader: TrackerMessageHeader{
			Action:        Announce,
			TransactionID: rand.Int31(),
		},
		// InfoHash[20]:  d.TorrentFile.InfoHash,
		// PeerID        [20]:    ,
		Downloaded: d.Downloaded,
		Left:       d.Left,
		Uploaded:   d.Uploaded,
		Event:      None,
		// IP            :    ,
		// Key           :    ,
		NumWant:    NumWant,
		Port:       0,
		Extensions: 0,
	}
	response := new(AnnounceResponse)
	rest, err := t.request(request, &response.announceResponse)
	if err != nil {
		return nil, err
	}
	if len(rest)%6 != 0 {
		return nil, errors.New("invalid peer list")
	}

	reader := bytes.NewReader(rest)
	count := len(rest) / 6
	response.Peers = make([]Peer, count)
	for i := 0; i < count; i++ {
		if err = binary.Read(reader, binary.BigEndian, &response.Peers[i]); err != nil {
			fmt.Println("--- HERE 1")
			return nil, err
		}
	}

	return response, nil
}

// request sends req to t and read the response into a buffer,
// does some checks on the response and copies into res.
// If the response is larger than res, remaining bytes are returned as rest.
func (t *Tracker) request(req, res TrackerMessage) (rest []byte, err error) {
	// A request should not take more that 60 seconds.
	err = t.conn.SetDeadline(time.Now().Add(60 * time.Second))
	if err != nil {
		return nil, err
	}

	// Send the request.
	err = binary.Write(t.conn, binary.BigEndian, req)
	if err != nil {
		return nil, err
	}

	// Read header first and check for error.
	// If the response is not an error, copy the bytes in buffer into res.
	var header TrackerMessageHeader

	// Read response into a buffer.
	n, err := t.conn.Read(t.buf)
	if err != nil {
		return nil, err
	}
	if n < binary.Size(header) {
		return nil, errors.New("response is too small")
	}
	fmt.Println("--- read", n, "bytes")
	// fmt.Println(string(t.buf[:n]))

	// Read header from buffer.
	reader := bytes.NewReader(t.buf)
	err = binary.Read(reader, binary.BigEndian, &header)
	if err != nil {
		return nil, err
	}
	if header.TransactionID != req.GetTransactionID() {
		return nil, errors.New("invalid transaction id")
	}

	// Tracker has sent and error instead of a response.
	if header.Action == Error {
		// The part after the header is the error message.
		return nil, TrackerError(t.buf[binary.Size(header):])
	}

	if n < binary.Size(res) {
		return nil, errors.New("response is smaller than expected")
	}

	// Copy the bytes into response struct and rest.
	reader.Seek(0, 0)
	err = binary.Read(reader, binary.BigEndian, res)
	if err != nil {
		fmt.Println("--- HERE 2")
		return nil, err
	}
	if rest != nil {
		reader.Read(rest)
	}
	return rest, nil
}

// TrackerMessageHeader contains the common fields in all TrackerMessage structs.
type TrackerMessageHeader struct {
	Action        Action
	TransactionID int32
}

func (r *TrackerMessageHeader) GetAction() Action       { return r.Action }
func (r *TrackerMessageHeader) GetTransactionID() int32 { return r.TransactionID }

// Requests can return a response or TrackerError.
type TrackerError string

func (e TrackerError) Error() string {
	return string(e)
}

// Close the tracker connection.
func (t *Tracker) Close() error {
	return t.conn.Close()
}

type TrackerMessage interface {
	GetAction() Action
	GetTransactionID() int32
}

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"reflect"
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
	ConnectionID  int64
	Action        Action
	TransactionID int32
}

type ConnectResponse struct {
	TrackerResponseHeader
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
		ConnectionID:  0x41727101980,
		Action:        Connect,
		TransactionID: rand.Int31(),
	}

	err = t.request(&request, &response)
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
	ConnectionID  int64
	Action        Action
	TransactionID int32
	InfoHash      [20]byte
	PeerID        [20]byte
	Downloaded    int64
	Left          int64
	Uploaded      int64
	Event         Event
	IP            uint32
	Key           uint32
	NumWant       int32
	Port          uint16
	Extensions    uint16
}

type AnnounceResponse struct {
	TrackerResponseHeader
	Interval int32
	Leechers int32
	Seeders  int32
	// Peers    [NumWant]Peer
}

type Peer struct {
	IP   int32
	Port uint16
}

func (t *Tracker) Announce(d *Download) (*AnnounceResponse, error) {
	request := &AnnounceRequest{
		ConnectionID:  t.ConnectionID,
		Action:        Announce,
		TransactionID: rand.Int31(),
		// InfoHash[20]:  d.TorrentFile.InfoHash,
		// PeerID        [20]:    ,
		Downloaded: d.Downloaded,
		Left:       d.Left,
		Uploaded:   d.Uploaded,
		Event:      None,
		// IP            :    ,
		// Key           :    ,
		NumWant:    5,
		Port:       0,
		Extensions: 0,
	}
	response := new(AnnounceResponse)
	return response, t.request(request, response)
}

func (t *Tracker) request(req interface{}, res interface{}) error {
	err := t.conn.SetDeadline(time.Now().Add(60 * time.Second))
	if err != nil {
		return err
	}

	err = binary.Write(t.conn, binary.BigEndian, req)
	if err != nil {
		return err
	}

	n, err := t.conn.Read(t.buf)
	if err != nil {
		return err
	}
	if n < headerSize {
		return errors.New("response is too small")
	}

	reader := bytes.NewReader(t.buf)

	var header TrackerResponseHeader
	err = binary.Read(reader, binary.BigEndian, &header)
	if err != nil {
		return err
	}
	if header.TransactionID != int32(reflect.ValueOf(req).Elem().FieldByName("TransactionID").Int()) {
		return errors.New("invalid transaction id")
	}

	reader.Seek(0, 0)

	if header.Action == Error {
		var te TrackerError
		err = binary.Read(reader, binary.BigEndian, &te)
		if err != nil {
			return err
		}
		return &te
	}

	if n < binary.Size(res) {
		return errors.New("response is smaller than expected")
	}

	return binary.Read(reader, binary.BigEndian, res)
}

type TrackerResponseHeader struct {
	Action        Action
	TransactionID int32
}

var headerSize = binary.Size(TrackerResponseHeader{})

type TrackerError struct {
	TrackerResponseHeader
	ErrorString []byte
}

func (e *TrackerError) Error() string {
	return string(e.ErrorString)
}

func (t *Tracker) Close() error {
	return t.conn.Close()
}

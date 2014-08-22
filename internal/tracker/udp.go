package tracker

// http://bittorrent.org/beps/bep_0015.html
// http://xbtt.sourceforge.net/udp_tracker_protocol.html
// http://www.rasterbar.com/products/libtorrent/udp_tracker_protocol.html

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/cenkalti/backoff"

	"github.com/cenkalti/rain/internal/protocol"
)

const connectionIDMagic = 0x41727101980

type trackerAction int32

// Tracker Actions
const (
	trackerActionConnect trackerAction = iota
	trackerActionAnnounce
	trackerActionScrape
	trackerActionError
)

type udpTracker struct {
	*trackerBase
	conn          *net.UDPConn
	transactions  map[int32]*transaction
	transactionsM sync.Mutex
	writeC        chan trackerRequest
}

type transaction struct {
	request  trackerRequest
	response []byte
	err      error
	done     chan struct{}
}

func newTransaction(req trackerRequest) *transaction {
	return &transaction{
		request: req,
		done:    make(chan struct{}),
	}
}

func (t *transaction) Done() {
	defer recover()
	close(t.done)
}

func newUDPTracker(b *trackerBase) *udpTracker {
	return &udpTracker{
		trackerBase:  b,
		transactions: make(map[int32]*transaction),
		writeC:       make(chan trackerRequest),
	}
}

func (t *udpTracker) Dial() error {
	serverAddr, err := net.ResolveUDPAddr("udp", t.url.Host)
	if err != nil {
		return err
	}
	t.conn, err = net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		return err
	}
	go t.readLoop()
	go t.writeLoop()
	return nil
}

// Close the tracker connection.
// TODO end all goroutines.
func (t *udpTracker) Close() error {
	return t.conn.Close()
}

// readLoop reads datagrams from connection, finds the transaction and
// sends the bytes to the transaction's response channel.
func (t *udpTracker) readLoop() {
	// Read buffer must be big enough to hold a UDP packet of maximum expected size.
	// Current value is: 320 = 20 + 50*6 (AnnounceResponse with 50 peers)
	buf := make([]byte, 320)
	for {
		n, err := t.conn.Read(buf)
		if err != nil {
			t.log.Error(err)
			if nerr, ok := err.(net.Error); ok && !nerr.Temporary() {
				t.log.Debug("End of tracker read loop")
				return
			}
			continue
		}
		t.log.Debug("Read ", n, " bytes")

		var header trackerMessageHeader
		if n < binary.Size(header) {
			t.log.Error("response is too small")
			continue
		}

		err = binary.Read(bytes.NewReader(buf), binary.BigEndian, &header)
		if err != nil {
			t.log.Error(err)
			continue
		}

		t.transactionsM.Lock()
		trx, ok := t.transactions[header.TransactionID]
		delete(t.transactions, header.TransactionID)
		t.transactionsM.Unlock()
		if !ok {
			t.log.Error("unexpected transaction_id")
			continue
		}

		// Tracker has sent and error.
		if header.Action == trackerActionError {
			// The part after the header is the error message.
			trx.err = trackerError(buf[binary.Size(header):])
			trx.Done()
			continue
		}

		// Copy data into a new slice because buf will be overwritten at next read.
		trx.response = make([]byte, n)
		copy(trx.response, buf)
		trx.Done()
	}
}

// writeLoop receives a request from t.transactionC, sets a random TransactionID
// and sends it to the tracker.
func (t *udpTracker) writeLoop() {
	var connectionID int64
	var connectionIDtime time.Time

	for req := range t.writeC {
		if time.Now().Sub(connectionIDtime) > 60*time.Second {
			connectionID = t.connect()
			connectionIDtime = time.Now()
		}
		req.SetConnectionID(connectionID)

		if err := binary.Write(t.conn, binary.BigEndian, req); err != nil {
			t.log.Error(err)
		}
	}
}

func (t *udpTracker) request(req trackerRequest, cancel <-chan struct{}) ([]byte, error) {
	action := func(req trackerRequest) { t.writeC <- req }
	return t.retry(req, action, cancel)
}

func (t *udpTracker) retry(req trackerRequest, action func(trackerRequest), cancel <-chan struct{}) ([]byte, error) {
	id := rand.Int31()
	req.SetTransactionID(id)

	trx := newTransaction(req)
	t.transactionsM.Lock()
	t.transactions[id] = trx
	t.transactionsM.Unlock()

	ticker := backoff.NewTicker(new(trackerUDPBackOff))
	for {
		select {
		case <-ticker.C:
			action(req)
		case <-trx.done:
			return trx.response, trx.err
		case <-cancel:
			return nil, errors.New("transaction cancelled")
		}
	}
}

type trackerMessage interface {
	GetAction() trackerAction
	SetAction(trackerAction)
	GetTransactionID() int32
	SetTransactionID(int32)
}

// trackerMessageHeader contains the common fields in all trackerMessage structs.
type trackerMessageHeader struct {
	Action        trackerAction
	TransactionID int32
}

func (h *trackerMessageHeader) GetAction() trackerAction  { return h.Action }
func (h *trackerMessageHeader) SetAction(a trackerAction) { h.Action = a }
func (h *trackerMessageHeader) GetTransactionID() int32   { return h.TransactionID }
func (h *trackerMessageHeader) SetTransactionID(id int32) { h.TransactionID = id }

type trackerRequestHeader struct {
	ConnectionID int64
	trackerMessageHeader
}

type trackerRequest interface {
	trackerMessage
	GetConnectionID() int64
	SetConnectionID(int64)
}

func (h *trackerRequestHeader) GetConnectionID() int64   { return h.ConnectionID }
func (h *trackerRequestHeader) SetConnectionID(id int64) { h.ConnectionID = id }

type trackerUDPBackOff int

func (b *trackerUDPBackOff) NextBackOff() time.Duration {
	defer func() { *b++ }()
	if *b > 8 {
		*b = 8
	}
	return time.Duration(15*(2^*b)) * time.Second
}

func (b *trackerUDPBackOff) Reset() { *b = 0 }

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
func (t *udpTracker) connect() int64 {
	req := new(connectRequest)
	req.SetConnectionID(connectionIDMagic)
	req.SetAction(trackerActionConnect)

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

		if response.Action != trackerActionConnect {
			t.log.Error("invalid action in connect response")
			continue
		}

		t.log.Debugf("connect Response: %#v\n", response)
		return response.ConnectionID
	}
}

type announceRequest struct {
	trackerRequestHeader
	InfoHash   protocol.InfoHash
	PeerID     protocol.PeerID
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
	trackerMessageHeader
	Interval int32
	Leechers int32
	Seeders  int32
}

// Announce announces transfer to t periodically.
func (t *udpTracker) Announce(transfer Transfer, cancel <-chan struct{}, event <-chan Event, responseC chan<- *AnnounceResponse) {
	err := t.Dial()
	if err != nil {
		// TODO retry connecting to tracker
		t.log.Fatal(err)
	}

	request := &announceRequest{
		InfoHash:   transfer.InfoHash(),
		PeerID:     t.peerID,
		Event:      None,
		IP:         0, // Tracker uses sender of this UDP packet.
		Key:        0, // TODO set it
		NumWant:    NumWant,
		Port:       t.port,
		Extensions: 0,
	}
	request.SetAction(trackerActionAnnounce)
	var nextAnnounce time.Duration = time.Nanosecond // Start immediately.
	for {
		select {
		// TODO send first without waiting
		case <-time.After(nextAnnounce):
			t.log.Debug("Time to announce")
			// TODO update on every try.
			request.update(transfer)

			// t.request may block, that's why we pass cancel as argument.
			reply, err := t.request(request, cancel)
			if err != nil {
				t.log.Error(err)
				continue
			}

			response, peers, err := t.parseAnnounceResponse(reply)
			if err != nil {
				t.log.Error(err)
				continue
			}
			t.log.Debugf("Announce response: %#v", response)

			// TODO calculate time and adjust.
			nextAnnounce = time.Duration(response.Interval) * time.Second

			announceResponse := &AnnounceResponse{
				// TODO handle error
				Interval: response.Interval,
				Leechers: response.Leechers,
				Seeders:  response.Seeders,
				Peers:    peers,
			}

			// may block if caller does not receive from it.
			select {
			case responseC <- announceResponse:
			case <-cancel:
				return
			}
		case <-cancel:
			return
			// case request.e = <-event:
			// request.update(d)
		}
	}
}

func (r *announceRequest) update(t Transfer) {
	r.Downloaded = t.Downloaded()
	r.Uploaded = t.Uploaded()
	r.Left = t.Left()
}

func (t *udpTracker) parseAnnounceResponse(data []byte) (*announceResponse, []Peer, error) {
	response := new(announceResponse)
	if len(data) < binary.Size(response) {
		return nil, nil, errors.New("response is too small")
	}

	reader := bytes.NewReader(data)

	err := binary.Read(reader, binary.BigEndian, response)
	if err != nil {
		return nil, nil, err
	}
	t.log.Debugf("annouceResponse: %#v", response)

	if response.Action != trackerActionAnnounce {
		return nil, nil, errors.New("invalid action")
	}

	peers, err := t.parsePeers(reader)
	if err != nil {
		return nil, nil, err
	}

	return response, peers, nil
}

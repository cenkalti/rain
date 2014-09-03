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
const connectionIDInterval = time.Minute

type action int32

// UDP tracker Actions
const (
	connect action = iota
	announce
	scrape
	errorAction
)

type transaction struct {
	request  udpReqeust
	response []byte
	err      error
	done     chan struct{}
}

func newTransaction(req udpReqeust) *transaction {
	req.SetTransactionID(rand.Int31())
	return &transaction{
		request: req,
		done:    make(chan struct{}),
	}
}

func (t *transaction) ID() int32 { return t.request.GetTransactionID() }
func (t *transaction) Done()     { close(t.done) }

type udpTracker struct {
	*trackerBase
	conn          *net.UDPConn
	dialMutex     sync.Mutex
	connected     bool
	transactions  map[int32]*transaction
	transactionsM sync.Mutex
	writeC        chan *transaction
}

func newUDPTracker(b *trackerBase) *udpTracker {
	return &udpTracker{
		trackerBase:  b,
		transactions: make(map[int32]*transaction),
		writeC:       make(chan *transaction),
	}
}

func (t *udpTracker) dial() error {
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

		var header udpMessageHeader
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
			t.log.Errorln("unexpected transaction_id:", header.TransactionID)
			continue
		}

		// Tracker has sent and error.
		if header.Action == errorAction {
			// The part after the header is the error message.
			trx.err = Error(buf[binary.Size(header):])
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

	for trx := range t.writeC {
		if time.Since(connectionIDtime) > connectionIDInterval {
			connectionID = t.connect()
			connectionIDtime = time.Now()
		}
		trx.request.SetConnectionID(connectionID)

		t.writeTrx(trx)
	}
}

func (t *udpTracker) writeTrx(trx *transaction) {
	t.log.Debugln("Writing transaction. ID:", trx.ID())
	err := binary.Write(t.conn, binary.BigEndian, trx.request)
	if err != nil {
		t.log.Error(err)
	}
}

// connect sends a connectRequest and returns a ConnectionID given by the tracker.
// On error, it backs off with the algorithm described in BEP15 and retries.
// It does not return until tracker sends a ConnectionID.
func (t *udpTracker) connect() int64 {
	req := new(connectRequest)
	req.SetAction(connect)
	req.SetConnectionID(connectionIDMagic)

	trx := newTransaction(req)

	for {
		data, err := t.retryTransaction(t.writeTrx, trx, nil) // Does not return until transaction is completed.
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

		if response.Action != connect {
			t.log.Error("invalid action in connect response")
			continue
		}

		t.log.Debugf("connect Response: %#v\n", response)
		return response.ConnectionID
	}
}

func (t *udpTracker) retryTransaction(f func(*transaction), trx *transaction, cancel <-chan struct{}) ([]byte, error) {
	t.transactionsM.Lock()
	t.transactions[trx.ID()] = trx
	t.transactionsM.Unlock()

	ticker := backoff.NewTicker(UDPBackOff())
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			f(trx)
		case <-trx.done:
			// transaction is deleted in readLoop()
			return trx.response, trx.err
		case <-cancel:
			t.transactionsM.Lock()
			delete(t.transactions, trx.ID())
			t.transactionsM.Unlock()
			return nil, errors.New("transaction cancelled")
		}
	}
}

func (t *udpTracker) sendTransaction(trx *transaction, cancel <-chan struct{}) ([]byte, error) {
	f := func(trx *transaction) { t.writeC <- trx }
	return t.retryTransaction(f, trx, cancel)
}

func (t *udpTracker) Announce(transfer Transfer, e Event) (*AnnounceResponse, error) {
	t.dialMutex.Lock()
	if !t.connected {
		err := t.dial()
		if err != nil {
			t.dialMutex.Unlock()
			return nil, err
		}
		t.connected = true
	}
	t.dialMutex.Unlock()

	request := &announceRequest{
		InfoHash:   transfer.InfoHash(),
		PeerID:     t.peerID,
		Event:      e,
		IP:         0, // Tracker uses sender of this UDP packet.
		Key:        0, // TODO set it
		NumWant:    NumWant,
		Port:       t.port,
		Extensions: 0,
	}
	request.SetAction(announce)

	// TODO update on every try.
	request.update(transfer)

	// t.request may block, that's why we pass cancel as argument.
	trx := newTransaction(request)
	reply, err := t.sendTransaction(trx, nil) // TODO pass cancel instead of nil
	if err != nil {
		return nil, err
	}

	response, peers, err := t.parseAnnounceResponse(reply)
	if err != nil {
		return nil, err
	}
	t.log.Debugf("Announce response: %#v", response)

	return &AnnounceResponse{
		Error:    nil, // TODO handler error
		Interval: time.Duration(response.Interval) * time.Second,
		Leechers: response.Leechers,
		Seeders:  response.Seeders,
		Peers:    peers,
	}, nil
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

	if response.Action != announce {
		return nil, nil, errors.New("invalid action")
	}

	peers, err := t.parsePeers(reader)
	if err != nil {
		return nil, nil, err
	}

	return response, peers, nil
}

type udpBackOff int

func (b *udpBackOff) NextBackOff() time.Duration {
	defer func() { *b++ }()
	if *b > 8 {
		*b = 8
	}
	return time.Duration(15*(2^*b)) * time.Second
}

func (b *udpBackOff) Reset() { *b = 0 }

var UDPBackOff = func() backoff.BackOff { return new(udpBackOff) }

type udpMessage interface {
	GetAction() action
	SetAction(action)
	GetTransactionID() int32
	SetTransactionID(int32)
}

type udpReqeust interface {
	udpMessage
	GetConnectionID() int64
	SetConnectionID(int64)
}

// udpMessageHeader implements udpMessage.
type udpMessageHeader struct {
	Action        action
	TransactionID int32
}

func (h *udpMessageHeader) GetAction() action         { return h.Action }
func (h *udpMessageHeader) SetAction(a action)        { h.Action = a }
func (h *udpMessageHeader) GetTransactionID() int32   { return h.TransactionID }
func (h *udpMessageHeader) SetTransactionID(id int32) { h.TransactionID = id }

// udpRequestHeader implements udpMessage and udpReqeust.
type udpRequestHeader struct {
	ConnectionID int64
	udpMessageHeader
}

func (h *udpRequestHeader) GetConnectionID() int64   { return h.ConnectionID }
func (h *udpRequestHeader) SetConnectionID(id int64) { h.ConnectionID = id }

type connectRequest struct {
	udpRequestHeader
}

type connectResponse struct {
	udpMessageHeader
	ConnectionID int64
}

type announceRequest struct {
	udpRequestHeader
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
	udpMessageHeader
	Interval int32
	Leechers int32
	Seeders  int32
}

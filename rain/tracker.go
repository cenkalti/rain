package rain

// http://bittorrent.org/beps/bep_0015.html
// http://xbtt.sourceforge.net/udp_tracker_protocol.html
// http://www.rasterbar.com/products/libtorrent/udp_tracker_protocol.html

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/rand"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/cenkalti/log"
)

const numWant = 50
const announcePort = 50000
const connectionIDMagic = 0x41727101980

type Action int32
type Event int32

// Tracker Actions
const (
	Connect Action = iota
	Announce
	Scrape
	Error
)

// Tracker Announce Events
const (
	None Event = iota
	Completed
	Started
	Stopped
)

type Tracker struct {
	URL           *url.URL
	peerID        *peerID
	conn          *net.UDPConn
	transactions  map[int32]*transaction
	transactionsM sync.Mutex
	writeC        chan TrackerRequest
}

type transaction struct {
	request  TrackerRequest
	response []byte
	err      error
	done     chan struct{}
}

func newTransaction(req TrackerRequest) *transaction {
	return &transaction{
		request: req,
		done:    make(chan struct{}),
	}
}

func (t *transaction) Done() {
	defer recover()
	close(t.done)
}

func NewTracker(trackerURL string, peerID *peerID) (*Tracker, error) {
	parsed, err := url.Parse(trackerURL)
	if err != nil {
		return nil, err
	}
	return &Tracker{
		URL:          parsed,
		peerID:       peerID,
		transactions: make(map[int32]*transaction),
		writeC:       make(chan TrackerRequest),
	}, nil
}

func (t *Tracker) Dial() error {
	serverAddr, err := net.ResolveUDPAddr("udp", t.URL.Host)
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
func (t *Tracker) Close() error {
	return t.conn.Close()
}

// readLoop reads datagrams from connection, finds the transaction and
// sends the bytes to the transaction's response channel.
func (t *Tracker) readLoop() {
	// Read buffer must be big enough to hold a UDP packet of maximum expected size.
	// Current value is: 320 = 20 + 50*6 (AnnounceResponse with 50 peers)
	buf := make([]byte, 320)
	for {
		n, err := t.conn.Read(buf)
		if err != nil {
			log.Error(err)
			if nerr, ok := err.(net.Error); ok && !nerr.Temporary() {
				log.Debug("--- end tracker read loop")
				return
			}
			continue
		}
		log.Debug("--- read", n, "bytes")

		var header TrackerMessageHeader
		if n < binary.Size(header) {
			log.Error("response is too small")
			continue
		}

		err = binary.Read(bytes.NewReader(buf), binary.BigEndian, &header)
		if err != nil {
			log.Error(err)
			continue
		}

		t.transactionsM.Lock()
		trx, ok := t.transactions[header.TransactionID]
		delete(t.transactions, header.TransactionID)
		t.transactionsM.Unlock()
		if !ok {
			log.Error("unexpected transaction_id")
			continue
		}

		// Tracker has sent and error.
		if header.Action == Error {
			// The part after the header is the error message.
			trx.err = TrackerError(buf[binary.Size(header):])
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
func (t *Tracker) writeLoop() {
	var connectionID int64
	var connectionIDtime time.Time

	for req := range t.writeC {
		if time.Now().Sub(connectionIDtime) > 60*time.Second {
			connectionID = t.connect()
			connectionIDtime = time.Now()
		}
		req.SetConnectionID(connectionID)

		if err := binary.Write(t.conn, binary.BigEndian, req); err != nil {
			log.Error(err)
		}
	}
}

func (t *Tracker) request(req TrackerRequest, cancel <-chan struct{}) ([]byte, error) {
	action := func(req TrackerRequest) { t.writeC <- req }
	return t.retry(req, action, cancel)
}

func (t *Tracker) retry(req TrackerRequest, action func(TrackerRequest), cancel <-chan struct{}) ([]byte, error) {
	id := rand.Int31()
	req.SetTransactionID(id)

	trx := newTransaction(req)
	t.transactionsM.Lock()
	t.transactions[id] = trx
	t.transactionsM.Unlock()

	ticker := backoff.NewTicker(new(TrackerUDPBackOff))
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

type TrackerMessage interface {
	GetAction() Action
	SetAction(Action)
	GetTransactionID() int32
	SetTransactionID(int32)
}

// TrackerMessageHeader contains the common fields in all TrackerMessage structs.
type TrackerMessageHeader struct {
	Action        Action
	TransactionID int32
}

func (h *TrackerMessageHeader) GetAction() Action         { return h.Action }
func (h *TrackerMessageHeader) SetAction(a Action)        { h.Action = a }
func (h *TrackerMessageHeader) GetTransactionID() int32   { return h.TransactionID }
func (h *TrackerMessageHeader) SetTransactionID(id int32) { h.TransactionID = id }

type TrackerRequestHeader struct {
	ConnectionID int64
	TrackerMessageHeader
}

type TrackerRequest interface {
	TrackerMessage
	GetConnectionID() int64
	SetConnectionID(int64)
}

func (h *TrackerRequestHeader) GetConnectionID() int64   { return h.ConnectionID }
func (h *TrackerRequestHeader) SetConnectionID(id int64) { h.ConnectionID = id }

// Requests can return a response or TrackerError.
type TrackerError string

func (e TrackerError) Error() string {
	return string(e)
}

type TrackerUDPBackOff int

func (b *TrackerUDPBackOff) NextBackOff() time.Duration {
	defer func() { *b++ }()
	if *b > 8 {
		*b = 8
	}
	return time.Duration(15*(2^*b)) * time.Second
}

func (b *TrackerUDPBackOff) Reset() { *b = 0 }

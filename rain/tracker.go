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
)

const numWant = 50
const connectionIDMagic = 0x41727101980

type trackerAction int32
type trackerEvent int32

// Tracker Actions
const (
	trackerActionConnect trackerAction = iota
	trackerActionAnnounce
	trackerActionScrape
	trackerActionError
)

// Tracker Announce Events
const (
	trackerEventNone trackerEvent = iota
	trackerEventCompleted
	trackerEventStarted
	trackerEventStopped
)

type tracker struct {
	URL           *url.URL
	peerID        peerID
	port          uint16
	conn          *net.UDPConn
	transactions  map[int32]*transaction
	transactionsM sync.Mutex
	writeC        chan trackerRequest
	log           logger
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

func newTracker(trackerURL string, peerID peerID, port uint16) (*tracker, error) {
	parsed, err := url.Parse(trackerURL)
	if err != nil {
		return nil, err
	}
	return &tracker{
		URL:          parsed,
		peerID:       peerID,
		port:         port,
		transactions: make(map[int32]*transaction),
		writeC:       make(chan trackerRequest),
		log:          newLogger("tracker " + trackerURL),
	}, nil
}

func (t *tracker) Dial() error {
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
func (t *tracker) Close() error {
	return t.conn.Close()
}

// readLoop reads datagrams from connection, finds the transaction and
// sends the bytes to the transaction's response channel.
func (t *tracker) readLoop() {
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
func (t *tracker) writeLoop() {
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

func (t *tracker) request(req trackerRequest, cancel <-chan struct{}) ([]byte, error) {
	action := func(req trackerRequest) { t.writeC <- req }
	return t.retry(req, action, cancel)
}

func (t *tracker) retry(req trackerRequest, action func(trackerRequest), cancel <-chan struct{}) ([]byte, error) {
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

// Requests can return a response or trackerError.
type trackerError string

func (e trackerError) Error() string {
	return string(e)
}

type trackerUDPBackOff int

func (b *trackerUDPBackOff) NextBackOff() time.Duration {
	defer func() { *b++ }()
	if *b > 8 {
		*b = 8
	}
	return time.Duration(15*(2^*b)) * time.Second
}

func (b *trackerUDPBackOff) Reset() { *b = 0 }

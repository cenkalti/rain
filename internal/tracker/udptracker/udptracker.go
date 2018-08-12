package udptracker

// http://bittorrent.org/beps/bep_0015.html
// http://xbtt.sourceforge.net/udp_tracker_bt.html
// http://www.rasterbar.com/products/libtorrent/udp_tracker_bt.html

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/cenkalti/backoff"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/tracker"
)

const connectionIDMagic = 0x41727101980
const connectionIDInterval = time.Minute

type UDPTracker struct {
	url           *url.URL
	log           logger.Logger
	conn          *net.UDPConn
	dialMutex     sync.Mutex
	connected     bool
	transactions  map[int32]*transaction
	transactionsM sync.Mutex
	writeC        chan *transaction
}

func New(u *url.URL) *UDPTracker {
	return &UDPTracker{
		url:          u,
		log:          logger.New("tracker " + u.String()),
		transactions: make(map[int32]*transaction),
		writeC:       make(chan *transaction),
	}
}

func (t *UDPTracker) dial() error {
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
func (t *UDPTracker) Close() error {
	close(t.writeC)
	if t.conn != nil {
		return t.conn.Close()
	}
	return nil
}

// readLoop reads datagrams from connection, finds the transaction and
// sends the bytes to the transaction's response channel.
func (t *UDPTracker) readLoop() {
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
		if header.Action == actionError {
			// The part after the header is the error message.
			trx.err = tracker.Error(buf[binary.Size(header):])
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
func (t *UDPTracker) writeLoop() {
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

func (t *UDPTracker) writeTrx(trx *transaction) {
	t.log.Debugln("Writing transaction. ID:", trx.ID())
	_, err := trx.request.WriteTo(t.conn)
	if err != nil {
		t.log.Error(err)
	}
}

// connect sends a connectRequest and returns a ConnectionID given by the tracker.
// On error, it backs off with the algorithm described in BEP15 and retries.
// It does not return until tracker sends a ConnectionID.
func (t *UDPTracker) connect() int64 {
	req := new(connectRequest)
	req.SetAction(actionConnect)
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

		if response.Action != actionConnect {
			t.log.Error("invalid action in connect response")
			continue
		}

		t.log.Debugf("connect Response: %#v\n", response)
		return response.ConnectionID
	}
}

func (t *UDPTracker) retryTransaction(f func(*transaction), trx *transaction, cancel <-chan struct{}) ([]byte, error) {
	t.transactionsM.Lock()
	t.transactions[trx.ID()] = trx
	t.transactionsM.Unlock()

	ticker := backoff.NewTicker(new(udpBackOff))
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
			return nil, tracker.ErrRequestCancelled
		}
	}
}

func (t *UDPTracker) sendTransaction(trx *transaction, cancel <-chan struct{}) ([]byte, error) {
	f := func(trx *transaction) { t.writeC <- trx }
	return t.retryTransaction(f, trx, cancel)
}

func (t *UDPTracker) Announce(transfer tracker.Transfer, e tracker.Event, cancel <-chan struct{}) (*tracker.AnnounceResponse, error) {
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
		PeerID:     transfer.PeerID(),
		Event:      e,
		IP:         0, // Tracker uses sender of this UDP packet.
		Key:        0, // TODO set it
		NumWant:    tracker.NumWant,
		Port:       uint16(transfer.Port()),
		Extensions: 0,
	}
	request.SetAction(actionAnnounce)

	request2 := &transferAnnounceRequest{
		transfer:        transfer,
		announceRequest: request,
		urlData:         t.url.RequestURI(),
	}
	trx := newTransaction(request2)

	// t.request may block, that's why we pass cancel as argument.
	reply, err := t.sendTransaction(trx, cancel)
	if err != nil {
		if err, ok := err.(tracker.Error); ok {
			return &tracker.AnnounceResponse{Error: err}, nil
		}
		return nil, err
	}

	response, peers, err := t.parseAnnounceResponse(reply)
	if err != nil {
		return nil, err
	}
	t.log.Debugf("Announce response: %#v", response)

	return &tracker.AnnounceResponse{
		Interval: time.Duration(response.Interval) * time.Second,
		Leechers: response.Leechers,
		Seeders:  response.Seeders,
		Peers:    peers,
	}, nil
}

func (t *UDPTracker) parseAnnounceResponse(data []byte) (*udpAnnounceResponse, []*net.TCPAddr, error) {
	response := new(udpAnnounceResponse)
	if len(data) < binary.Size(response) {
		return nil, nil, errors.New("response is too small")
	}

	reader := bytes.NewReader(data)

	err := binary.Read(reader, binary.BigEndian, response)
	if err != nil {
		return nil, nil, err
	}
	t.log.Debugf("annouceResponse: %#v", response)

	if response.Action != actionAnnounce {
		return nil, nil, errors.New("invalid action")
	}

	peers, err := tracker.ParsePeersBinary(reader, t.log)
	if err != nil {
		return nil, nil, err
	}

	return response, peers, nil
}

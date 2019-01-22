package udptracker

// http://bittorrent.org/beps/bep_0015.html
// http://xbtt.sourceforge.net/udp_tracker_protocol.html

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/session/blocklist"
)

const connectionIDMagic = 0x41727101980
const connectionIDInterval = time.Minute

type Transport struct {
	blocklist blocklist.Blocklist
	conn      *net.UDPConn
	log       logger.Logger

	connections  map[string]*connection
	transactions map[int32]*transaction
	m            sync.Mutex

	closeC chan struct{}
}

type connection struct {
	id        int64
	timestamp time.Time
	m         sync.Mutex
}

func NewTransport(bl blocklist.Blocklist) *Transport {
	return &Transport{
		blocklist:    bl,
		log:          logger.New("udp tracker transport"),
		connections:  make(map[string]*connection),
		transactions: make(map[int32]*transaction),
		closeC:       make(chan struct{}),
	}
}

func (t *Transport) getConnection(addr string) *connection {
	t.m.Lock()
	defer t.m.Unlock()
	conn, ok := t.connections[addr]
	if !ok {
		conn = new(connection)
		t.connections[addr] = conn
	}
	return conn
}

func (t *Transport) listen() error {
	t.m.Lock()
	defer t.m.Unlock()

	if t.conn != nil {
		return nil
	}

	var laddr net.UDPAddr
	conn, err := net.ListenUDP("udp4", &laddr)
	if err != nil {
		return err
	}

	t.conn = conn
	go t.readLoop()
	return nil
}

func (t *Transport) Do(ctx context.Context, trx *transaction) ([]byte, error) {
	err := t.listen()
	if err != nil {
		return nil, err
	}
	ip, port, err := tracker.ResolveHost(ctx, trx.dest, t.blocklist)
	if err != nil {
		return nil, err
	}
	trx.addr = &net.UDPAddr{IP: ip, Port: port}

	conn := t.getConnection(trx.addr.String())
	if time.Since(conn.timestamp) > connectionIDInterval {
		conn.id, err = t.connect(ctx, trx.addr)
		if err != nil {
			return nil, err
		}
		conn.timestamp = time.Now()
	}
	trx.request.SetConnectionID(conn.id)
	return t.retryTransaction(ctx, t.writeTrx, trx)
}

// Close the tracker connection.
func (t *Transport) Close() error {
	close(t.closeC)
	if t.conn != nil {
		return t.conn.Close()
	}
	return nil
}

// readLoop reads datagrams from connection, finds the transaction and
// sends the bytes to the transaction's response channel.
func (t *Transport) readLoop() {
	// Read buffer must be big enough to hold a UDP packet of maximum expected size.
	// Current value is: 320 = 20 + 50*6 (AnnounceResponse with 50 peers)
	const maxNumWant = 1000
	bigBuf := make([]byte, 20+6*maxNumWant)
	for {
		n, err := t.conn.Read(bigBuf)
		if err != nil {
			select {
			case <-t.closeC:
			default:
				t.log.Error(err)
			}
			return
		}
		t.log.Debug("Read ", n, " bytes")
		buf := bigBuf[:n]

		var header udpMessageHeader
		err = binary.Read(bytes.NewReader(buf), binary.BigEndian, &header)
		if err != nil {
			t.log.Error(err)
			continue
		}

		t.m.Lock()
		trx, ok := t.transactions[header.TransactionID]
		delete(t.transactions, header.TransactionID)
		t.m.Unlock()
		if !ok {
			t.log.Debugln("unexpected transaction_id:", header.TransactionID)
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
		trx.response = make([]byte, len(buf))
		copy(trx.response, buf)
		trx.Done()
	}
}

func (t *Transport) writeTrx(trx *transaction) {
	t.log.Debugln("Writing transaction. ID:", trx.ID())
	var buf bytes.Buffer
	_, err := trx.request.WriteTo(&buf)
	if err != nil {
		t.log.Error(err)
		return
	}
	_, err = t.conn.WriteTo(buf.Bytes(), trx.addr)
	if err != nil {
		t.log.Error(err)
	}
}

// connect sends a connectRequest and returns a ConnectionID given by the tracker.
// On error, it backs off with the algorithm described in BEP15 and retries.
// It does not return until tracker sends a reply.
func (t *Transport) connect(ctx context.Context, addr net.Addr) (connectionID int64, err error) {
	req := new(connectRequest)
	req.SetAction(actionConnect)
	req.SetConnectionID(connectionIDMagic)

	trx := newTransaction(req, "")
	trx.addr = addr

	for {
		data, err := t.retryTransaction(ctx, t.writeTrx, trx) // Does not return until transaction is completed.
		if err != nil {
			return 0, err
		}

		var response connectResponse
		err = binary.Read(bytes.NewReader(data), binary.BigEndian, &response)
		if err != nil {
			return 0, err
		}

		if response.Action != actionConnect {
			return 0, errors.New("invalid action in connect response")
		}

		t.log.Debugf("connect Response: %#v\n", response)
		return response.ConnectionID, nil
	}
}

func (t *Transport) retryTransaction(ctx context.Context, f func(*transaction), trx *transaction) ([]byte, error) {
	t.m.Lock()
	t.transactions[trx.ID()] = trx
	t.m.Unlock()

	ticker := backoff.NewTicker(new(udpBackOff))
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			f(trx)
		case <-trx.done:
			// transaction is deleted in readLoop()
			return trx.response, trx.err
		case <-ctx.Done():
			t.m.Lock()
			delete(t.transactions, trx.ID())
			t.m.Unlock()
			return nil, context.Canceled
		}
	}
}

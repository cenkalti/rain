package udptracker

// http://bittorrent.org/beps/bep_0015.html
// http://xbtt.sourceforge.net/udp_tracker_protocol.html

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/tracker"
)

const connectionIDMagic = 0x41727101980
const connectionIDInterval = time.Minute

type Transport struct {
	host          string
	log           logger.Logger
	conn          net.Conn
	dialMutex     sync.Mutex
	connected     bool
	transactions  map[int32]*transaction
	transactionsM sync.Mutex // TODO remove udp tracker mutex
	writeC        chan *transaction
	closeC        chan struct{}
}

func NewTransport(host string) *Transport {
	return &Transport{
		host:         host,
		log:          logger.New("udp tracker " + host),
		transactions: make(map[int32]*transaction),
		writeC:       make(chan *transaction),
		closeC:       make(chan struct{}),
	}
}

func (t *Transport) Do(ctx context.Context, trx *transaction) ([]byte, error) {
	err := t.maybeDial(ctx)
	if err != nil {
		return nil, err
	}
	f := func(trx *transaction) {
		select {
		case t.writeC <- trx:
		case <-ctx.Done():
		}
	}
	return t.retryTransaction(ctx, f, trx)
}

func (t *Transport) maybeDial(ctx context.Context) error {
	t.dialMutex.Lock()
	defer t.dialMutex.Unlock()
	if t.connected {
		return nil
	}
	err := t.dial(ctx)
	if err != nil {
		return err
	}
	t.connected = true
	return nil
}

// Close the tracker connection.
func (t *Transport) Close() error {
	close(t.closeC)
	if t.conn != nil {
		return t.conn.Close()
	}
	return nil
}

func (t *Transport) dial(ctx context.Context) error {
	var dialer net.Dialer
	var err error
	t.conn, err = dialer.DialContext(ctx, "udp", t.host)
	if err != nil {
		return err
	}
	go t.readLoop()
	go t.writeLoop()
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

		t.transactionsM.Lock()
		trx, ok := t.transactions[header.TransactionID]
		delete(t.transactions, header.TransactionID)
		t.transactionsM.Unlock()
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

// writeLoop receives a request from t.transactionC, sets a random TransactionID
// and sends it to the tracker.
func (t *Transport) writeLoop() {
	var connectionID int64
	var connectionIDtime time.Time

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-t.closeC
		cancel()
	}()

	for {
		select {
		case trx := <-t.writeC:
			if time.Since(connectionIDtime) > connectionIDInterval {
				var canceled bool
				connectionID, canceled = t.connect(ctx)
				if canceled {
					return
				}
				connectionIDtime = time.Now()
			}
			trx.request.SetConnectionID(connectionID)
			t.writeTrx(trx)
		case <-t.closeC:
			return
		}
	}
}

func (t *Transport) writeTrx(trx *transaction) {
	t.log.Debugln("Writing transaction. ID:", trx.ID())
	_, err := trx.request.WriteTo(t.conn)
	if err != nil {
		t.log.Error(err)
	}
}

// connect sends a connectRequest and returns a ConnectionID given by the tracker.
// On error, it backs off with the algorithm described in BEP15 and retries.
// It does not return until tracker sends a ConnectionID.
func (t *Transport) connect(ctx context.Context) (connectionID int64, canceled bool) {
	req := new(connectRequest)
	req.SetAction(actionConnect)
	req.SetConnectionID(connectionIDMagic)

	trx := newTransaction(req)

	for {
		data, err := t.retryTransaction(ctx, t.writeTrx, trx) // Does not return until transaction is completed.
		if err == context.Canceled {
			return 0, true
		} else if err != nil {
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
		return response.ConnectionID, false
	}
}

func (t *Transport) retryTransaction(ctx context.Context, f func(*transaction), trx *transaction) ([]byte, error) {
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
		case <-ctx.Done():
			t.transactionsM.Lock()
			delete(t.transactions, trx.ID())
			t.transactionsM.Unlock()
			return nil, context.Canceled
		}
	}
}

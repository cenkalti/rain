package udptracker

// http://bittorrent.org/beps/bep_0015.html
// http://xbtt.sourceforge.net/udp_tracker_protocol.html

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"slices"
	"strconv"
	"time"

	"github.com/cenkalti/rain/v2/internal/blocklist"
	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/cenkalti/rain/v2/internal/resolver"
	"github.com/cenkalti/rain/v2/internal/tracker"
	"github.com/zeebo/bencode"
)

const (
	connectionIDMagic    = 0x41727101980
	connectionIDInterval = time.Minute
	// Allow the retries at 15, 45 and 105 seconds, then let the tracker tier advance.
	announceTimeout = 2 * time.Minute
)

// Transport for UDP tracker implementation.
type Transport struct {
	blocklist  *blocklist.Blocklist
	log        logger.Logger
	dnsTimeout time.Duration

	// Transport.Do will send messages to this channel.
	requestC chan *transportRequest
	cancelC  chan *transportRequest

	// read loop will send data read from UDP connection and send them to this channel.
	readC chan []byte

	// Will be closed by Transport.Close to end Transport.Run loop.
	closeC chan struct{}

	// This channel will be closed after Transport.Run loop ends.
	doneC chan struct{}
}

// NewTransport returns a new UDP tracker transport.
func NewTransport(bl *blocklist.Blocklist, dnsTimeout time.Duration) *Transport {
	return &Transport{
		blocklist:  bl,
		log:        logger.New("udp tracker transport"),
		dnsTimeout: dnsTimeout,
		requestC:   make(chan *transportRequest),
		cancelC:    make(chan *transportRequest),
		readC:      make(chan []byte),
		closeC:     make(chan struct{}),
		doneC:      make(chan struct{}),
	}
}

// Close the transport.
func (t *Transport) Close() {
	close(t.closeC)
	<-t.doneC
}

// Do sends the transaction to the tracker. Retries on failure.
func (t *Transport) Do(req *transportRequest) ([]byte, error) {
	var errTransportClosed = errors.New("udp transport closed")
	ctx, cancel := context.WithTimeout(req.ctx, announceTimeout)
	defer cancel()
	req.ctx = ctx

	select {
	case t.requestC <- req:
	case <-req.ctx.Done():
		return nil, req.ctx.Err()
	case <-t.closeC:
		return nil, errTransportClosed
	}
	defer func() {
		select {
		case t.cancelC <- req:
		case <-t.doneC:
		}
	}()

	select {
	case <-req.done:
		return req.response, req.err
	case <-req.ctx.Done():
		return nil, req.ctx.Err()
	case <-t.closeC:
		return nil, errTransportClosed
	}

}

func (t *Transport) Run() {
	t.log.Debugln("Starting transport run loop")
	var laddr net.UDPAddr
	udpConn, listenErr := net.ListenUDP("udp4", &laddr)
	if listenErr != nil {
		t.run(nil, listenErr)
		return
	}
	t.run(udpConn, nil)
}

func (t *Transport) run(udpConn net.PacketConn, listenErr error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var listening bool
	if listenErr != nil {
		t.log.Error(listenErr)
	} else {
		listening = true
		t.log.Debugln("Starting transport read loop")
		go t.readLoop(udpConn)
	}

	// All transaction are saved with their ID as key.
	transactions := make(map[int32]*transaction)
	// Connections can be either connecting or connected.
	connections := make(map[string]*connection)
	connectDone := make(chan *connectionResult)
	connectionExpired := make(chan *connection)
	transactionExpired := make(chan expiredTransaction)

	// Transaction can be either a connection request or announce request.
	beginTransaction := func(i udpRequest) (*transaction, error) {
		trx := newTransaction(i)
		_, ok := transactions[trx.id]
		if ok {
			trx.cancel()
			return nil, errors.New("transaction id collision")
		}
		transactions[trx.id] = trx
		return trx, nil
	}

	startRequest := func(req *transportRequest) {
		if req.ctx.Err() != nil {
			return
		}
		// Requests cannot proceed if the UDP listener failed.
		if !listening {
			req.SetResponse(nil, listenErr)
			return
		}
		conn, ok := connections[req.dest]
		if ok && !conn.connectedAt.IsZero() && time.Since(conn.connectedAt) >= connectionIDInterval {
			delete(connections, req.dest)
			ok = false
		}
		if !ok {
			conn = newConnection(ctx, req)
			connections[req.dest] = conn
			req.connection = conn
			trx, err := beginTransaction(conn)
			if err != nil {
				delete(connections, req.dest)
				req.SetResponse(nil, err)
			} else {
				conn.transaction = trx
				go resolveDestinationAndConnect(trx, req.dest, udpConn, t.dnsTimeout, t.blocklist, connectDone, t.closeC)
			}
		} else {
			req.connection = conn
			if !conn.connectedAt.IsZero() {
				req.ConnectionID = conn.id
				trx, err := beginTransaction(req)
				if err != nil {
					req.SetResponse(nil, err)
				} else {
					req.transaction = trx
					go retryTransaction(trx, udpConn, conn.addr, req.retryBackoff, conn.connectedAt.Add(connectionIDInterval), transactionExpired)
				}
			} else {
				// Connection is in connecting state.
				conn.requests = append(conn.requests, req)
			}
		}
	}

	for {
		select {
		case req := <-t.requestC:
			startRequest(req)
		case req := <-t.cancelC:
			if trx := req.transaction; trx != nil {
				delete(transactions, trx.id)
				trx.cancel()
				req.transaction = nil
			}
			conn := req.connection
			if conn == nil || !conn.connectedAt.IsZero() || connections[conn.dest] != conn {
				break
			}
			for i, pending := range conn.requests {
				if pending == req {
					conn.requests = slices.Delete(conn.requests, i, i+1)
					break
				}
			}
			if len(conn.requests) == 0 {
				delete(connections, conn.dest)
				delete(transactions, conn.transaction.id)
				conn.transaction.cancel()
			}
		case expired := <-transactionExpired:
			trx := expired.trx
			if transactions[trx.id] != trx {
				break
			}
			delete(transactions, trx.id)
			trx.cancel()
			req := trx.request.(*transportRequest)
			req.transaction = nil
			req.retryBackoff = expired.backoff
			startRequest(req)
		case res := <-connectDone:
			conn := res.trx.request.(*connection)

			// Transaction must be finished, successful or not.
			if transactions[res.trx.id] == res.trx {
				delete(transactions, res.trx.id)
			}
			res.trx.cancel()
			if connections[res.dest] != conn {
				break
			}

			// Handle connection error.
			if res.err != nil {
				// Notify all requests waiting for connection about the error.
				delete(connections, res.dest)
				for _, req := range conn.requests {
					req.SetResponse(nil, res.err)
				}
				break
			}

			// We're connected. Set connection details.
			conn.addr = res.addr
			conn.id = res.id
			conn.connectedAt = res.connectedAt

			// Expire the connection after defined period.
			go func(conn *connection) {
				select {
				case <-time.After(connectionIDInterval):
				case <-t.closeC:
					return
				}
				select {
				case connectionExpired <- conn:
				case <-t.closeC:
				}
			}(conn)

			// Start announce transaction for all waiting requests.
			for _, req := range conn.requests {
				if req.ctx.Err() != nil {
					continue
				}
				req.ConnectionID = conn.id
				if req.retryBackoff > 0 {
					// Reconnecting must not restart an announce's retry schedule.
					req.retryBackoff = max(req.retryBackoff, res.retryBackoff)
				}
				trx, err := beginTransaction(req)
				if err != nil {
					req.SetResponse(nil, err)
				} else {
					req.transaction = trx
					go retryTransaction(trx, udpConn, conn.addr, req.retryBackoff, conn.connectedAt.Add(connectionIDInterval), transactionExpired)
				}
			}

			// All requests are sent, clear waiting request list.
			conn.requests = nil
		case conn := <-connectionExpired:
			if connections[conn.dest] == conn {
				delete(connections, conn.dest)
			}
		case buf := <-t.readC:
			var header udpMessageHeader
			err := binary.Read(bytes.NewReader(buf), binary.BigEndian, &header)
			if err != nil {
				t.log.Error(err)
				continue
			}

			trx, ok := transactions[header.TransactionID]
			if !ok {
				t.log.Debugln("Unexpected transaction ID:", header.TransactionID)
				continue
			}
			delete(transactions, header.TransactionID)
			if req, ok := trx.request.(*transportRequest); ok {
				req.transaction = nil
			}

			t.log.Debugln("Received response for transaction ID:", header.TransactionID)

			// Tracker has sent and error.
			if header.Action == actionError {
				// The part after the header is the error message.
				rest := buf[binary.Size(header):]
				var terr struct {
					FailureReason string `bencode:"failure reason"`
					RetryIn       string `bencode:"retry in"`
				}
				err = bencode.DecodeBytes(rest, &terr)
				if err != nil {
					err = tracker.ErrDecode
				} else {
					retryIn, _ := strconv.Atoi(terr.RetryIn)
					err = &tracker.Error{
						FailureReason: terr.FailureReason,
						RetryIn:       time.Duration(retryIn) * time.Minute,
					}
				}
			}
			trx.request.SetResponse(buf, err)
			trx.cancel()
		case <-t.closeC:
			for _, conn := range connections {
				for _, req := range conn.requests {
					req.SetResponse(nil, errors.New("transport closing"))
				}
			}
			for _, trx := range transactions {
				trx.cancel()
			}
			if listening {
				udpConn.Close()
			}
			close(t.doneC)
			return
		}
	}
}

// readLoop reads datagrams from connection and sends to the run loop.
func (t *Transport) readLoop(conn net.PacketConn) {
	// Read buffer must be big enough to hold a UDP packet of maximum expected size.
	const maxNumWant = 1000
	bigBuf := make([]byte, 20+6*maxNumWant)
	for {
		n, _, err := conn.ReadFrom(bigBuf)
		if err != nil {
			select {
			case <-t.closeC:
			default:
				t.log.Error(err)
			}
			return
		}
		t.log.Debug("Read ", n, " bytes")
		buf := make([]byte, n)
		copy(buf, bigBuf)
		select {
		case t.readC <- buf:
		case <-t.closeC:
			return
		}

	}
}

type connectionResult struct {
	trx          *transaction
	dest         string
	addr         *net.UDPAddr
	id           int64
	err          error
	connectedAt  time.Time
	retryBackoff udpBackOff
}

func resolveDestinationAndConnect(trx *transaction, dest string, udpConn net.PacketConn, dnsTimeout time.Duration, blocklist *blocklist.Blocklist, resultC chan *connectionResult, stopC chan struct{}) {
	res := &connectionResult{
		trx:  trx,
		dest: dest,
	}

	ip, port, err := resolver.Resolve(trx.ctx, dest, dnsTimeout, blocklist)
	if err != nil {
		res.err = err
		select {
		case resultC <- res:
		case <-stopC:
		}
		return
	}

	res.addr = &net.UDPAddr{IP: ip, Port: port}

	res.id, res.retryBackoff, res.err = sendAndReceiveConnect(trx, udpConn, res.addr)
	if res.err == nil {
		res.connectedAt = time.Now()
	}

	select {
	case resultC <- res:
	case <-stopC:
	}
}

func sendAndReceiveConnect(trx *transaction, conn net.PacketConn, addr net.Addr) (int64, udpBackOff, error) {
	// Send request until the transaction is canceled.
	retryDone := make(chan udpBackOff, 1)
	go func() {
		retryDone <- retryTransaction(trx, conn, addr, trx.request.(*connection).retryBackoff, time.Time{}, nil)
	}()
	// Wait until transaction is finished.
	select {
	case <-trx.request.(*connection).done:
	case <-trx.ctx.Done():
	}
	trx.cancel()
	retryBackoff := <-retryDone
	// Receiving a response also cancels the transaction.
	select {
	case <-trx.request.(*connection).done:
	default:
		return 0, retryBackoff, trx.ctx.Err()
	}

	data, err := trx.request.GetResponse()
	if err != nil {
		return 0, retryBackoff, err
	}

	var response connectResponse
	err = binary.Read(bytes.NewReader(data), binary.BigEndian, &response)
	if err != nil {
		return 0, retryBackoff, err
	}

	if response.Action != actionConnect {
		return 0, retryBackoff, errors.New("invalid action in connect response")
	}

	return response.ConnectionID, retryBackoff, nil
}

type expiredTransaction struct {
	trx     *transaction
	backoff udpBackOff
}

// Send the request until the transaction is canceled.
// Transaction canceled by either via outer context or when response is received from the tracker.
// It backs off with the algorithm described in BEP15 and retries.
func retryTransaction(trx *transaction, conn net.PacketConn, addr net.Addr, retryBackoff udpBackOff, expiresAt time.Time, expiredC chan<- expiredTransaction) udpBackOff {
	var b bytes.Buffer
	_, _ = trx.request.WriteTo(&b)
	data := b.Bytes()

	timer := time.NewTimer(0)
	defer timer.Stop()
	nextBackoff := retryBackoff
	for {
		select {
		case <-timer.C:
			if trx.ctx.Err() != nil {
				return retryBackoff
			}
			retryBackoff = nextBackoff
			if !expiresAt.IsZero() && !time.Now().Before(expiresAt) {
				// BEP 15 requires a new connection ID before retrying an expired announce.
				select {
				case expiredC <- expiredTransaction{trx: trx, backoff: retryBackoff}:
				case <-trx.ctx.Done():
				}
				return retryBackoff
			}
			_, _ = conn.WriteTo(data, addr)
			timer.Reset(nextBackoff.NextBackOff())
		case <-trx.ctx.Done():
			return retryBackoff
		}
	}
}

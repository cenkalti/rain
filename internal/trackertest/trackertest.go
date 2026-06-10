// Package trackertest provides minimal in-memory BitTorrent trackers for use
// in tests. It speaks just enough of the HTTP (BEP 3) and UDP (BEP 15) announce
// protocols to exercise rain's tracker clients and end-to-end download flow,
// without pulling in an external tracker implementation.
package trackertest

import (
	"encoding/binary"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/cenkalti/rain/v2/internal/tracker"
	"github.com/zeebo/bencode"
)

// announceInterval is the interval advertised to clients in announce responses.
const announceInterval = time.Minute

// peerStore keeps the set of peers seen for each info hash. A peer is keyed by
// its compact (IP+port) form so a re-announce updates the same entry. It is
// safe for concurrent use.
type peerStore struct {
	mu    sync.Mutex
	peers map[[20]byte]map[tracker.CompactPeer]struct{}
}

func newPeerStore() *peerStore {
	return &peerStore{peers: make(map[[20]byte]map[tracker.CompactPeer]struct{})}
}

// announce records the announcing peer (or removes it on a "stopped" event),
// then returns up to numWant other peers in the swarm, excluding the announcer
// itself. A numWant <= 0 means no limit.
func (s *peerStore) announce(infoHash [20]byte, self tracker.CompactPeer, stopped bool, numWant int) []tracker.CompactPeer {
	s.mu.Lock()
	defer s.mu.Unlock()

	swarm := s.peers[infoHash]
	if swarm == nil {
		swarm = make(map[tracker.CompactPeer]struct{})
		s.peers[infoHash] = swarm
	}
	if stopped {
		delete(swarm, self)
	} else {
		swarm[self] = struct{}{}
	}

	var result []tracker.CompactPeer
	for p := range swarm {
		if p == self {
			continue
		}
		if numWant > 0 && len(result) >= numWant {
			break
		}
		result = append(result, p)
	}
	return result
}

// compactPeer builds a CompactPeer from an IP address and port. The bool is
// false when the address is not IPv4, in which case the caller ignores it.
func compactPeer(ip net.IP, port int) (tracker.CompactPeer, bool) {
	v4 := ip.To4()
	if v4 == nil {
		return tracker.CompactPeer{}, false
	}
	return tracker.NewCompactPeer(&net.TCPAddr{IP: v4, Port: port}), true
}

// encodeCompactPeers concatenates the 6-byte compact form of each peer.
func encodeCompactPeers(peers []tracker.CompactPeer) []byte {
	b := make([]byte, 0, len(peers)*6)
	for _, p := range peers {
		encoded, _ := p.MarshalBinary()
		b = append(b, encoded...)
	}
	return b
}

// HTTPTracker is an HTTP BitTorrent tracker (BEP 3) for use in tests.
type HTTPTracker struct {
	store  *peerStore
	server *http.Server
	doneC  chan struct{}
}

// NewHTTP starts an HTTP tracker listening on addr (e.g. "127.0.0.1:5000"). It
// serves /announce; the returned tracker must be stopped with Close.
func NewHTTP(addr string) (*HTTPTracker, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	t := &HTTPTracker{
		store: newPeerStore(),
		doneC: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/announce", t.handleAnnounce)
	t.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	}
	go func() {
		_ = t.server.Serve(l)
		close(t.doneC)
	}()
	return t, nil
}

// Close stops the HTTP tracker and waits for the serving goroutine to exit.
func (t *HTTPTracker) Close() error {
	err := t.server.Close()
	<-t.doneC
	return err
}

type httpAnnounceResponse struct {
	Interval int    `bencode:"interval"`
	Peers    []byte `bencode:"peers"`
}

func (t *HTTPTracker) handleAnnounce(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var infoHash [20]byte
	copy(infoHash[:], q.Get("info_hash"))

	port64, err := strconv.ParseUint(q.Get("port"), 10, 16)
	if err != nil {
		http.Error(w, "invalid port", http.StatusBadRequest)
		return
	}
	port := int(port64)
	numWant, _ := strconv.Atoi(q.Get("numwant"))
	stopped := q.Get("event") == "stopped"

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	self, ok := compactPeer(net.ParseIP(host), port)
	if !ok {
		http.Error(w, "non-IPv4 peer", http.StatusBadRequest)
		return
	}

	peers := t.store.announce(infoHash, self, stopped, numWant)

	resp := httpAnnounceResponse{
		Interval: int(announceInterval / time.Second),
		Peers:    encodeCompactPeers(peers),
	}
	w.Header().Set("Content-Type", "text/plain")
	_ = bencode.NewEncoder(w).Encode(resp)
}

// BEP 15 (UDP tracker) protocol constants.
const (
	udpProtocolID  = 0x41727101980
	actionConnect  = 0
	actionAnnounce = 1
	actionError    = 3
	eventStopped   = 3
	// udpConnectionID is the (arbitrary) connection id handed to clients in the
	// connect response and echoed back in the announce request.
	udpConnectionID = 0x27101980
)

// UDPTracker is a UDP BitTorrent tracker (BEP 15) for use in tests.
type UDPTracker struct {
	store *peerStore
	conn  *net.UDPConn
	doneC chan struct{}
}

// NewUDP starts a UDP tracker listening on addr (e.g. "127.0.0.1:5000"). The
// returned tracker must be stopped with Close.
func NewUDP(addr string) (*UDPTracker, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	t := &UDPTracker{
		store: newPeerStore(),
		conn:  conn,
		doneC: make(chan struct{}),
	}
	go t.serve()
	return t, nil
}

// Close stops the UDP tracker and waits for the serving goroutine to exit.
func (t *UDPTracker) Close() error {
	err := t.conn.Close()
	<-t.doneC
	return err
}

func (t *UDPTracker) serve() {
	defer close(t.doneC)
	buf := make([]byte, 2048)
	for {
		n, addr, err := t.conn.ReadFromUDP(buf)
		if err != nil {
			return // connection closed
		}
		t.handle(buf[:n], addr)
	}
}

func (t *UDPTracker) handle(req []byte, addr *net.UDPAddr) {
	// Every request begins with: connection_id (8) action (4) transaction_id (4).
	if len(req) < 16 {
		return
	}
	action := binary.BigEndian.Uint32(req[8:12])
	transactionID := binary.BigEndian.Uint32(req[12:16])

	switch action {
	case actionConnect:
		if binary.BigEndian.Uint64(req[0:8]) != udpProtocolID {
			t.writeError(addr, transactionID, "invalid protocol id")
			return
		}
		resp := make([]byte, 16)
		binary.BigEndian.PutUint32(resp[0:4], actionConnect)
		binary.BigEndian.PutUint32(resp[4:8], transactionID)
		binary.BigEndian.PutUint64(resp[8:16], udpConnectionID)
		_, _ = t.conn.WriteToUDP(resp, addr)
	case actionAnnounce:
		t.handleAnnounce(req, addr, transactionID)
	default:
		t.writeError(addr, transactionID, "unknown action")
	}
}

func (t *UDPTracker) handleAnnounce(req []byte, addr *net.UDPAddr, transactionID uint32) {
	// Announce request layout (BEP 15), 98 bytes before optional extensions:
	//   [16:36] info_hash, [80:84] event, [92:96] num_want, [96:98] port.
	if len(req) < 98 {
		t.writeError(addr, transactionID, "short announce")
		return
	}
	var infoHash [20]byte
	copy(infoHash[:], req[16:36])
	event := binary.BigEndian.Uint32(req[80:84])
	numWant := int32(binary.BigEndian.Uint32(req[92:96]))
	port := int(binary.BigEndian.Uint16(req[96:98]))

	// The IP field ([84:88]) is 0 in rain's requests, so use the source IP.
	self, ok := compactPeer(addr.IP, port)
	if !ok {
		t.writeError(addr, transactionID, "non-IPv4 peer")
		return
	}

	peers := t.store.announce(infoHash, self, event == eventStopped, int(numWant))

	compact := encodeCompactPeers(peers)
	resp := make([]byte, 20+len(compact))
	binary.BigEndian.PutUint32(resp[0:4], actionAnnounce)
	binary.BigEndian.PutUint32(resp[4:8], transactionID)
	binary.BigEndian.PutUint32(resp[8:12], uint32(announceInterval/time.Second))
	binary.BigEndian.PutUint32(resp[12:16], 0)                  // leechers
	binary.BigEndian.PutUint32(resp[16:20], uint32(len(peers))) // seeders
	copy(resp[20:], compact)
	_, _ = t.conn.WriteToUDP(resp, addr)
}

func (t *UDPTracker) writeError(addr *net.UDPAddr, transactionID uint32, msg string) {
	resp := make([]byte, 8, 8+len(msg))
	binary.BigEndian.PutUint32(resp[0:4], actionError)
	binary.BigEndian.PutUint32(resp[4:8], transactionID)
	resp = append(resp, msg...)
	_, _ = t.conn.WriteToUDP(resp, addr)
}

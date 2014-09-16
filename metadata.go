package rain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"sync"
	"time"

	"github.com/cenkalti/log"
	"github.com/zeebo/bencode"

	"github.com/cenkalti/rain/internal/magnet"
	"github.com/cenkalti/rain/internal/protocol"
	"github.com/cenkalti/rain/internal/torrent"
	"github.com/cenkalti/rain/internal/tracker"
)

const (
	metadataPieceSize      = 16 * 1024
	metadataNetworkTimeout = 2 * time.Minute
)

// Extension IDs
const (
	extensionHandshakeID = iota
	extensionMetadataID
)

// Metadata Extension Message Types
const (
	metadataRequest = iota
	metadataData
	metadataReject
)

type MetadataDownloader struct {
	magnet    *magnet.Magnet
	tracker   tracker.Tracker
	announceC chan *tracker.AnnounceResponse
	Result    chan *torrent.Info
	cancel    chan struct{}
	peers     map[tracker.Peer]struct{} // connecting or connected
	peersM    sync.Mutex
}

func NewMetadataDownloader(m *magnet.Magnet) (*MetadataDownloader, error) {
	if len(m.Trackers) == 0 {
		return nil, errors.New("magnet link does not contain a tracker")
	}
	c, err := newDummyClient()
	if err != nil {
		return nil, err
	}
	tr, err := tracker.New(m.Trackers[0], c)
	if err != nil {
		return nil, err
	}
	return &MetadataDownloader{
		magnet:    m,
		tracker:   tr,
		announceC: make(chan *tracker.AnnounceResponse),
		Result:    make(chan *torrent.Info, 1),
		cancel:    make(chan struct{}),
		peers:     make(map[tracker.Peer]struct{}),
	}, nil
}

func (m *MetadataDownloader) Run(announceInterval time.Duration) {
	t := emptyTransfer(m.magnet.InfoHash)
	events := make(chan tracker.Event)
	go tracker.AnnouncePeriodically(m.tracker, &t, m.cancel, tracker.None, events, m.announceC)
	for {
		select {
		case resp := <-m.announceC:
			log.Infof("Seeders: %d Leechers: %d", resp.Seeders, resp.Leechers)
			for _, p := range resp.Peers {
				go m.worker(p)
			}
		case <-time.After(announceInterval):
			select {
			case events <- tracker.None:
			default:
			}
		}
	}
}

func (m *MetadataDownloader) worker(peer tracker.Peer) {
	// Do not open multiple connections to the same peer simultaneously.
	m.peersM.Lock()
	if _, ok := m.peers[peer]; ok {
		m.peersM.Unlock()
		return
	}
	m.peers[peer] = struct{}{}
	defer func() {
		m.peersM.Lock()
		delete(m.peers, peer)
		m.peersM.Unlock()
	}()
	m.peersM.Unlock()

	conn, err := net.DialTCP("tcp4", nil, peer.TCPAddr())
	if err != nil {
		log.Error(err)
		return
	}

	p := newPeer(conn)
	p.log.Debug("tcp connection is opened")

	info, err := downloadMetadataFromPeer(m.magnet, p)
	conn.Close()
	if err != nil {
		p.log.Error(err)
		return
	}

	select {
	case m.Result <- info:
		close(m.cancel) // will stop other workers
	case <-m.cancel:
		return
	}
}

func downloadMetadataFromPeer(m *magnet.Magnet, p *peer) (*torrent.Info, error) {
	err := p.conn.SetDeadline(time.Now().Add(metadataNetworkTimeout))
	if err != nil {
		return nil, err
	}

	peerID, err := generatePeerID()
	if err != nil {
		return nil, err
	}

	extensions := [8]byte{}
	extensions[5] |= 0x10 // BEP 10 Extension Protocol

	err = p.sendHandShake(m.InfoHash, peerID, extensions)
	if err != nil {
		p.log.Debug("cannot send BT handshake")
		return nil, err
	}
	p.log.Debug("sent BT handshake")

	ex, ih, err := readHandShake1(p.conn)
	if err != nil {
		p.log.Debug("cannot read handshake part 1")
		return nil, err
	}
	if *ih != m.InfoHash {
		return nil, errors.New("unexpected info_hash")
	}
	if ex.Bytes()[5]&0x10 == 0 {
		return nil, errors.New("extension protocol is not supported by peer")
	}

	id, err := readHandShake2(p.conn)
	if err != nil {
		p.log.Debug("cannot read handshake part 2")
		return nil, err
	}
	if *id == peerID {
		return nil, errors.New("rejected own connection: client")
	}

	p.log.Debug("BT handshake completed")

	// Extension Protocol Handshake
	d := &extensionHandshakeMessage{
		M: extensionMapping{
			UTMetadata: extensionMetadataID,
		},
	}

	err = p.sendExtensionHandshake(d)
	if err != nil {
		return nil, err
	}
	p.log.Debug("Sent extension handshake")

	var (
		v             extensionHandshakeMessage
		metadataBytes []byte
		numPieces     uint32
		lastPieceSize uint32
		remaining     uint32
	)

	for {
		err = p.conn.SetDeadline(time.Now().Add(metadataNetworkTimeout))
		if err != nil {
			return nil, err
		}

		var length uint32
		err = binary.Read(p.conn, binary.BigEndian, &length)
		if err != nil {
			return nil, err
		}
		if length == 0 { // keep-alive
			continue
		}

		var messageID protocol.MessageType
		err = binary.Read(p.conn, binary.BigEndian, &messageID)
		if err != nil {
			return nil, err
		}
		length--

		if messageID != protocol.Extension { // extension message id
			io.CopyN(ioutil.Discard, p.conn, int64(length))
			continue
		}
		p.log.Debugln("Read extension message")

		var extensionID uint8
		err = binary.Read(p.conn, binary.BigEndian, &extensionID)
		if err != nil {
			return nil, err
		}
		length--
		p.log.Debugln("LTEP message ID:", extensionID)

		switch extensionID {
		case extensionHandshakeID:
			payload := make([]byte, length)
			_, err = io.ReadFull(p.conn, payload)
			if err != nil {
				return nil, err
			}

			r := bytes.NewReader(payload)
			d := bencode.NewDecoder(r)
			err = d.Decode(&v)
			if err != nil {
				return nil, err
			}

			if v.M.UTMetadata == 0 {
				return nil, errors.New("ut_metadata extension is not supported")
			}

			if v.MetadataSize == 0 {
				return nil, errors.New("zero metadata size")
			}
			p.log.Infoln("Metadata size:", v.MetadataSize, "bytes")

			metadataBytes = make([]byte, v.MetadataSize)
			numPieces = v.MetadataSize / (metadataPieceSize)
			lastPieceSize = v.MetadataSize - (numPieces * metadataPieceSize)
			if lastPieceSize > 0 {
				numPieces++
			}
			remaining = numPieces
			p.log.Infoln("Metadata has", numPieces, "piece(s)")
			if numPieces == 1 {
				lastPieceSize = v.MetadataSize
			}

			// Send metadata piece requests.
			for i := uint32(0); i < numPieces; i++ {
				m := &metadataMessage{
					MessageType: metadataRequest,
					Piece:       i,
				}
				err = sendMetadataMessage(m, p, v.M.UTMetadata)
				if err != nil {
					return nil, err
				}
				p.log.Debugln("piece request sent", i)
			}
		case extensionMetadataID:
			payload := make([]byte, length)
			_, err = io.ReadFull(p.conn, payload)
			if err != nil {
				return nil, err
			}

			r := bytes.NewReader(payload)
			decoder := bencode.NewDecoder(r)

			in := make(map[string]uint32)
			err = decoder.Decode(&in)
			if err != nil {
				return nil, err
			}

			msgType, ok := in["msg_type"]
			if !ok {
				return nil, errors.New("no msg_type field in metadata message")
			}
			p.log.Debugln("msg_type:", msgType)

			i, ok := in["piece"]
			if !ok {
				return nil, errors.New("no piece field in metadata message")
			}
			if i >= numPieces {
				return nil, fmt.Errorf("metadata has %d pieces but peer sent piece #%d", numPieces, i)
			}

			switch msgType {
			case metadataRequest:
				req := &metadataMessage{
					MessageType: metadataReject,
					Piece:       i,
				}
				sendMetadataMessage(req, p, v.M.UTMetadata)
			case metadataData:
				var expectedSize uint32
				if i == numPieces-1 {
					expectedSize = lastPieceSize
				} else {
					expectedSize = metadataPieceSize
				}

				piece := payload[decoder.BytesParsed():]
				if uint32(len(piece)) != expectedSize {
					return nil, fmt.Errorf("received piece smaller than expected (%d/%d)", len(piece), expectedSize)
				}

				copy(metadataBytes[i*metadataPieceSize:], piece)

				remaining--
				if remaining == 0 {
					info, err := torrent.NewInfo(metadataBytes)
					if err != nil {
						return nil, err
					}
					if m.InfoHash != info.Hash {
						return nil, errors.New("invalid metadata received")
					}
					p.log.Info("peer has successfully sent the metadata")
					return info, nil
				}
			case metadataReject:
				return nil, errors.New("peer rejected our metadata request")
			}
		}
	}
}

func sendMetadataMessage(m *metadataMessage, p *peer, id uint8) error {
	var buf bytes.Buffer
	e := bencode.NewEncoder(&buf)
	err := e.Encode(m)
	if err != nil {
		return err
	}
	return p.sendExtensionMessage(id, buf.Bytes())
}

type extensionHandshakeMessage struct {
	M            extensionMapping `bencode:"m"`
	MetadataSize uint32           `bencode:"metadata_size,omitempty"`
}

type extensionMapping struct {
	UTMetadata uint8 `bencode:"ut_metadata"`
}

type metadataMessage struct {
	MessageType uint8  `bencode:"msg_type"`
	Piece       uint32 `bencode:"piece"`
}

type dummyClient struct {
	peerID protocol.PeerID
}

func newDummyClient() (*dummyClient, error) {
	var c dummyClient
	var err error
	c.peerID, err = generatePeerID()
	return &c, err
}

func (c *dummyClient) PeerID() protocol.PeerID { return c.peerID }
func (c *dummyClient) Port() uint16            { return 6881 }

// Required to make a fake announce to tracker to get peer list for metadata download.
type emptyTransfer protocol.InfoHash

func (t *emptyTransfer) InfoHash() protocol.InfoHash { return protocol.InfoHash(*t) }
func (t *emptyTransfer) Downloaded() int64           { return 0 }
func (t *emptyTransfer) Uploaded() int64             { return 0 }
func (t *emptyTransfer) Left() int64                 { return metadataPieceSize } // trackers don't accept 0

package rain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"time"

	"github.com/cenkalti/log"
	"github.com/zeebo/bencode"

	"github.com/cenkalti/rain/internal/protocol"
	"github.com/cenkalti/rain/internal/torrent"
	"github.com/cenkalti/rain/internal/tracker"
)

const (
	metadataPieceSize           = 16 * 1024
	concurrentMetadataDownloads = 1000
	metadataNetworkTimeout      = 2 * time.Minute
)

func DownloadMetadata(m *Magnet) (*torrent.Info, error) {
	tr, err := tracker.New(m.Trackers[0], protocol.PeerID{}, 0)
	if err != nil {
		return nil, err
	}

	t := emptyTransfer(m.InfoHash)
	announceC := make(chan *tracker.AnnounceResponse)
	go tr.Announce(&t, nil, nil, announceC)

	peerC := make(chan tracker.Peer)
	resultC := make(chan *torrent.Info)
	cancel := make(chan struct{})

	go func() {
		for resp := range announceC {
			for _, p := range resp.Peers {
				peerC <- p
			}
		}
	}()

	for i := 0; i < concurrentMetadataDownloads; i++ {
		go metadataDownloader(m, peerC, resultC, cancel)
	}

	defer func() { close(cancel) }()
	return <-resultC, nil
}

func metadataDownloader(m *Magnet, peers chan tracker.Peer, result chan *torrent.Info, cancel chan struct{}) {
	for {
		select {
		case peer := <-peers:
			conn, err := net.DialTCP("tcp4", nil, peer.TCPAddr())
			if err != nil {
				log.Error(err)
				continue
			}

			p := newPeer(conn)
			p.log.Debug("tcp connection is opened")

			info, err := downloadMetadataFromPeer(m, p)
			conn.Close()
			if err != nil {
				p.log.Error(err)
				continue
			}

			select {
			case result <- info:
			case <-cancel:
				return
			}
		case <-cancel:
			return
		}
	}
}

func downloadMetadataFromPeer(m *Magnet, p *peer) (*torrent.Info, error) {
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

	ex, ih, err := p.readHandShake1()
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

	id, err := p.readHandShake2()
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
			UTMetadata: 1,
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

		var extensionID byte
		err = binary.Read(p.conn, binary.BigEndian, &extensionID)
		if err != nil {
			return nil, err
		}
		length--
		p.log.Debugln("LTEP message ID:", extensionID)

		switch extensionID {
		case 0: // handshake
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

			metadataBytes = make([]byte, v.MetadataSize)
			numPieces = v.MetadataSize / (metadataPieceSize)
			lastPieceSize = v.MetadataSize - (numPieces * metadataPieceSize)
			if lastPieceSize > 0 {
				numPieces++
			}
			remaining = numPieces
			p.log.Debugln("metadata has", numPieces, "pieces")

			// Send metadata piece requests.
			for i := uint32(0); i < numPieces; i++ {
				m := &metadataMessage{
					MessageType: 0,
					Piece:       i,
				}
				err = sendMetadataMessage(m, p, v.M.UTMetadata)
				if err != nil {
					return nil, err
				}
				p.log.Debugln("piece request sent", i)
			}
		case 1: // ut_metadata
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
			case 0: // request
				req := &metadataMessage{
					MessageType: 2,
					Piece:       i,
				}
				sendMetadataMessage(req, p, v.M.UTMetadata)
			case 1: // data
				var expectedSize uint32
				if i == numPieces-1 {
					expectedSize = lastPieceSize
				} else {
					expectedSize = metadataPieceSize
				}

				piece := payload[decoder.BytesParsed():]
				if uint32(len(piece)) != expectedSize {
					return nil, errors.New("received piece smaller than expected")
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
			case 2: // reject
				return nil, errors.New("peer rejected our metadata request")
			}
		}
	}
}

func sendMetadataMessage(m *metadataMessage, p *peer, id byte) error {
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
	UTMetadata byte `bencode:"ut_metadata"`
}

type metadataMessage struct {
	MessageType byte   `bencode:"msg_type"`
	Piece       uint32 `bencode:"piece"`
}

// Required to make a fake announce to tracker to get peer list for metadata download.
type emptyTransfer protocol.InfoHash

func (t *emptyTransfer) InfoHash() protocol.InfoHash { return protocol.InfoHash(*t) }
func (t *emptyTransfer) Downloaded() int64           { return 0 }
func (t *emptyTransfer) Uploaded() int64             { return 0 }
func (t *emptyTransfer) Left() int64                 { return metadataPieceSize } // trackers don't accept 0

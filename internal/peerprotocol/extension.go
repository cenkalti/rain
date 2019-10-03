package peerprotocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"github.com/zeebo/bencode"
)

const (
	// ExtensionIDHandshake is ID for extension handshake message.
	ExtensionIDHandshake = iota
	// ExtensionIDMetadata is ID for metadata extension messages.
	ExtensionIDMetadata
	// ExtensionIDPEX is ID for PEX extension messages.
	ExtensionIDPEX
)

const (
	// ExtensionKeyMetadata is the key for the metadata extension.
	ExtensionKeyMetadata = "ut_metadata"
	// ExtensionKeyPEX is the key for the PEX extension.
	ExtensionKeyPEX = "ut_pex"
)

const (
	// ExtensionMetadataMessageTypeRequest is the id of metadata message when requesting a piece.
	ExtensionMetadataMessageTypeRequest = iota
	// ExtensionMetadataMessageTypeData is the id of metadata message when sending the piece data.
	ExtensionMetadataMessageTypeData
	// ExtensionMetadataMessageTypeReject is the id of metadata message when rejecting a piece.
	ExtensionMetadataMessageTypeReject
)

// ExtensionMessage is extension to BitTorrent protocol.
type ExtensionMessage struct {
	ExtendedMessageID uint8
	Payload           interface{}
}

// ID returns the type of a peer message.
func (m ExtensionMessage) ID() MessageID { return Extension }

// Read extension message bytes.
func (m ExtensionMessage) Read([]byte) (int, error) {
	panic("Read must not be called, use WriteTo")
}

// WriteTo writes the bytes into io.Writer.
func (m ExtensionMessage) WriteTo(w io.Writer) (n int64, err error) {
	nn, err := w.Write([]byte{m.ExtendedMessageID})
	n += int64(nn)
	if err != nil {
		return
	}
	wc := newWriterCounter(w)
	err = bencode.NewEncoder(wc).Encode(m.Payload)
	n += wc.Count()
	if err != nil {
		return
	}
	if mm, ok := m.Payload.(ExtensionMetadataMessage); ok {
		nn, err = w.Write(mm.Data)
		n += int64(nn)
	}
	return
}

// UnmarshalBinary parses extension message.
func (m *ExtensionMessage) UnmarshalBinary(data []byte) error {
	var extID uint8
	r := bytes.NewReader(data)
	err := binary.Read(r, binary.BigEndian, &extID)
	if err != nil {
		return err
	}
	m.ExtendedMessageID = extID
	payload := data[1:]
	dec := bencode.NewDecoder(bytes.NewReader(payload))
	switch m.ExtendedMessageID {
	case ExtensionIDHandshake:
		var extMsg ExtensionHandshakeMessage
		err = dec.Decode(&extMsg)
		m.Payload = extMsg
		if extMsg.MetadataSize < 0 {
			extMsg.MetadataSize = 0
		}
		if extMsg.RequestQueue < 0 {
			extMsg.RequestQueue = 0
		}
	case ExtensionIDMetadata:
		var extMsg ExtensionMetadataMessage
		err = dec.Decode(&extMsg)
		extMsg.Data = payload[dec.BytesParsed():]
		m.Payload = extMsg
	case ExtensionIDPEX:
		var extMsg ExtensionPEXMessage
		err = dec.Decode(&extMsg)
		m.Payload = extMsg
	default:
		return fmt.Errorf("peer sent invalid extension message id: %d", m.ExtendedMessageID)
	}
	return err
}

// ExtensionHandshakeMessage contains the information to do the extension handshake.
type ExtensionHandshakeMessage struct {
	M            map[string]uint8 `bencode:"m"`
	V            string           `bencode:"v"`
	YourIP       string           `bencode:"yourip,omitempty"`
	MetadataSize int              `bencode:"metadata_size,omitempty"`
	RequestQueue int              `bencode:"reqq"`
}

// NewExtensionHandshake returns a new ExtensionHandshakeMessage by filling the struct with given values.
func NewExtensionHandshake(metadataSize uint32, version string, yourip net.IP, requestQueueLength int) ExtensionHandshakeMessage {
	return ExtensionHandshakeMessage{
		M: map[string]uint8{
			ExtensionKeyMetadata: ExtensionIDMetadata,
			ExtensionKeyPEX:      ExtensionIDPEX,
		},
		V:            version,
		YourIP:       string(truncateIP(yourip)),
		MetadataSize: int(metadataSize),
		RequestQueue: requestQueueLength,
	}
}

// ExtensionMetadataMessage is the message for the Metadata extension.
type ExtensionMetadataMessage struct {
	Type      int    `bencode:"msg_type"`
	Piece     uint32 `bencode:"piece"`
	TotalSize int    `bencode:"total_size,omitempty"`
	Data      []byte `bencode:"-"`
}

// ExtensionPEXMessage is the message for the PEX extension.
type ExtensionPEXMessage struct {
	Added   string `bencode:"added"`
	Dropped string `bencode:"dropped"`
}

func truncateIP(ip net.IP) net.IP {
	ip4 := ip.To4()
	if ip4 != nil {
		return ip4
	}
	return ip
}

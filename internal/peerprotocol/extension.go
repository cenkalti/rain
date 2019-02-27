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
	ExtensionIDHandshake = iota
	ExtensionIDMetadata
	ExtensionIDPEX
)

const (
	ExtensionKeyMetadata = "ut_metadata"
	ExtensionKeyPEX      = "ut_pex"
)

const (
	ExtensionMetadataMessageTypeRequest = iota
	ExtensionMetadataMessageTypeData
	ExtensionMetadataMessageTypeReject
)

type ExtensionMessage struct {
	ExtendedMessageID uint8
	Payload           interface{}
}

func (m ExtensionMessage) ID() MessageID { return Extension }

func (m ExtensionMessage) Read([]byte) (int, error) {
	panic("read must not be called")
}

func (m ExtensionMessage) WriteTo(w io.Writer) (int64, error) {
	w.Write([]byte{m.ExtendedMessageID})
	err := bencode.NewEncoder(w).Encode(m.Payload)
	if err != nil {
		return 0, err
	}
	if mm, ok := m.Payload.(ExtensionMetadataMessage); ok {
		_, err = w.Write(mm.Data)
	}
	return 0, err
}

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

type ExtensionHandshakeMessage struct {
	M            map[string]uint8 `bencode:"m"`
	V            string           `bencode:"v"`
	YourIP       string           `bencode:"yourip,omitempty"`
	MetadataSize uint32           `bencode:"metadata_size,omitempty"`
}

func NewExtensionHandshake(metadataSize uint32, version string, yourip net.IP) ExtensionHandshakeMessage {
	return ExtensionHandshakeMessage{
		M: map[string]uint8{
			ExtensionKeyMetadata: ExtensionIDMetadata,
			ExtensionKeyPEX:      ExtensionIDPEX,
		},
		V:            version,
		YourIP:       string(truncateIP(yourip)),
		MetadataSize: metadataSize,
	}
}

type ExtensionMetadataMessage struct {
	Type      uint32 `bencode:"msg_type"`
	Piece     uint32 `bencode:"piece"`
	TotalSize uint32 `bencode:"total_size,omitempty"`
	Data      []byte `bencode:"-"`
}

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

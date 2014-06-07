package rain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"time"
)

type AnnounceRequest struct {
	TrackerRequestHeader
	InfoHash   [20]byte
	PeerID     [20]byte
	Downloaded int64
	Left       int64
	Uploaded   int64
	Event      Event
	IP         uint32
	Key        uint32
	NumWant    int32
	Port       uint16
	Extensions uint16
}

type AnnounceResponse struct {
	announceResponse
	Peers []*Peer
}

type announceResponse struct {
	TrackerMessageHeader
	Interval int32
	Leechers int32
	Seeders  int32
}

type Peer struct {
	IP   [net.IPv4len]byte
	Port uint16
}

func (p Peer) TCPAddr() *net.TCPAddr {
	ip := make(net.IP, net.IPv4len)
	copy(ip, p.IP[:])
	return &net.TCPAddr{
		IP:   ip,
		Port: int(p.Port),
	}
}

// announce announces d to t periodically.
func (t *Tracker) announce(d *Download, cancel <-chan struct{}, event <-chan Event, responseC chan<- *AnnounceResponse) {
	defer func() {
		if responseC != nil {
			close(responseC)
		}
	}()
	request := &AnnounceRequest{
		InfoHash:   d.TorrentFile.InfoHash,
		PeerID:     t.peerID,
		Event:      None,
		IP:         0, // Tracker uses sender of this UDP packet.
		Key:        0, // TODO set it
		NumWant:    NumWant,
		Port:       AnnouncePort,
		Extensions: 0,
	}
	request.SetAction(Announce)
	response := new(AnnounceResponse)
	var nextAnnounce time.Duration = time.Nanosecond // Start immediately.
	for {
		select {
		// TODO send first without waiting
		case <-time.After(nextAnnounce):
			fmt.Println("--- announce")
			// TODO update on every try.
			request.update(d)

			// t.request may block, that's why we pass cancel as argument.
			reply, err := t.request(request, cancel)
			if err != nil {
				log.Println(err)
				continue
			}

			if err = response.Load(reply); err != nil {
				log.Println(err)
				continue
			}

			// TODO calculate time and adjust.
			nextAnnounce = time.Duration(response.Interval) * time.Second

			// may block if caller does not receive from it.
			select {
			case responseC <- response:
			case <-cancel:
				return
			}
		case <-cancel:
			return
			// case request.e = <-event:
			// request.update(d)
		}
	}
}

func (r *AnnounceRequest) update(d *Download) {
	r.Downloaded = d.Downloaded
	r.Uploaded = d.Uploaded
	r.Left = d.Left()
}

func (r *AnnounceResponse) Load(data []byte) error {
	if len(data) < binary.Size(r) {
		return errors.New("response is too small")
	}

	reader := bytes.NewReader(data)

	err := binary.Read(reader, binary.BigEndian, &r.announceResponse)
	if err != nil {
		return err
	}
	fmt.Printf("--- r.announceResponse: %#v\n", r.announceResponse)

	if r.Action != Announce {
		return errors.New("invalid action")
	}

	fmt.Printf("--- len(rest): %#v\n", reader.Len())
	if reader.Len()%6 != 0 {
		return errors.New("invalid peer list")
	}

	count := reader.Len() / 6
	fmt.Printf("--- count: %#v\n", count)
	r.Peers = make([]*Peer, count)
	for i := 0; i < count; i++ {
		r.Peers[i] = new(Peer)
		if err = binary.Read(reader, binary.BigEndian, r.Peers[i]); err != nil {
			return err
		}
	}
	fmt.Printf("--- r.Peers: %#v\n", r.Peers)

	return nil
}

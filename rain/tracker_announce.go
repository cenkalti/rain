package rain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"time"
)

type announceRequest struct {
	trackerRequestHeader
	InfoHash   infoHash
	PeerID     peerID
	Downloaded int64
	Left       int64
	Uploaded   int64
	Event      trackerEvent
	IP         uint32
	Key        uint32
	NumWant    int32
	Port       uint16
	Extensions uint16
}

type announceResponse struct {
	announceResponseBase
	Peers []*peerAddr
}

type announceResponseBase struct {
	trackerMessageHeader
	Interval int32
	Leechers int32
	Seeders  int32
}

type peerAddr struct {
	IP   [net.IPv4len]byte
	Port uint16
}

func (p peerAddr) TCPAddr() *net.TCPAddr {
	ip := make(net.IP, net.IPv4len)
	copy(ip, p.IP[:])
	return &net.TCPAddr{
		IP:   ip,
		Port: int(p.Port),
	}
}

// announce announces d to t periodically.
func (t *tracker) announce(d *transfer, cancel <-chan struct{}, event <-chan trackerEvent, responseC chan<- *announceResponse) {
	defer func() {
		if responseC != nil {
			close(responseC)
		}
	}()
	request := &announceRequest{
		InfoHash:   d.torrentFile.Info.Hash,
		PeerID:     t.peerID,
		Event:      trackerEventNone,
		IP:         0, // Tracker uses sender of this UDP packet.
		Key:        0, // TODO set it
		NumWant:    numWant,
		Port:       t.port,
		Extensions: 0,
	}
	request.SetAction(trackerActionAnnounce)
	response := new(announceResponse)
	var nextAnnounce time.Duration = time.Nanosecond // Start immediately.
	for {
		select {
		// TODO send first without waiting
		case <-time.After(nextAnnounce):
			t.log.Debug("Time to announce")
			// TODO update on every try.
			request.update(d)

			// t.request may block, that's why we pass cancel as argument.
			reply, err := t.request(request, cancel)
			if err != nil {
				t.log.Error(err)
				continue
			}

			if err = t.Load(response, reply); err != nil {
				t.log.Error(err)
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

func (r *announceRequest) update(d *transfer) {
	r.Downloaded = d.Downloaded
	r.Uploaded = d.Uploaded
	r.Left = d.Left()
}

func (t *tracker) Load(r *announceResponse, data []byte) error {
	if len(data) < binary.Size(r) {
		return errors.New("response is too small")
	}

	reader := bytes.NewReader(data)

	err := binary.Read(reader, binary.BigEndian, &r.announceResponseBase)
	if err != nil {
		return err
	}
	t.log.Debugf("r.announceResponseBase: %#v", r.announceResponseBase)

	if r.Action != trackerActionAnnounce {
		return errors.New("invalid action")
	}

	t.log.Debugf("len(rest): %#v", reader.Len())
	if reader.Len()%6 != 0 {
		return errors.New("invalid peer list")
	}

	count := reader.Len() / 6
	t.log.Debugf("count of peers: %#v", count)
	r.Peers = make([]*peerAddr, count)
	for i := 0; i < count; i++ {
		r.Peers[i] = new(peerAddr)
		if err = binary.Read(reader, binary.BigEndian, r.Peers[i]); err != nil {
			return err
		}
	}
	t.log.Debugf("r.Peers: %#v\n", r.Peers)

	return nil
}

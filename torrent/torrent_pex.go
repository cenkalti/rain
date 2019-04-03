package torrent

import "net"

func (t *torrent) pexAddPeer(addr *net.TCPAddr) {
	if !t.session.config.PEXEnabled {
		return
	}
	for pe := range t.peers {
		if pe.PEX != nil {
			pe.PEX.Add(addr)
		}
	}
}

func (t *torrent) pexDropPeer(addr *net.TCPAddr) {
	if !t.session.config.PEXEnabled {
		return
	}
	for pe := range t.peers {
		if pe.PEX != nil {
			pe.PEX.Drop(addr)
		}
	}
}

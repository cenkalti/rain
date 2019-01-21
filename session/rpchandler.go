package session

import (
	"encoding/base64"
	"errors"
	"strings"

	"github.com/cenkalti/rain/rainrpc"
)

type rpcHandler struct {
	session *Session
}

func (h *rpcHandler) ListTorrents(args *rainrpc.ListTorrentsRequest, reply *rainrpc.ListTorrentsResponse) error {
	torrents := h.session.ListTorrents()
	reply.Torrents = make([]rainrpc.Torrent, 0, len(torrents))
	for _, t := range torrents {
		reply.Torrents = append(reply.Torrents, newTorrent(t))
	}
	return nil
}

func (h *rpcHandler) AddTorrent(args *rainrpc.AddTorrentRequest, reply *rainrpc.AddTorrentResponse) error {
	r := base64.NewDecoder(base64.StdEncoding, strings.NewReader(args.Torrent))
	t, err := h.session.AddTorrent(r)
	if err != nil {
		return err
	}
	reply.Torrent = newTorrent(t)
	return nil
}

func (h *rpcHandler) AddMagnet(args *rainrpc.AddMagnetRequest, reply *rainrpc.AddMagnetResponse) error {
	t, err := h.session.AddMagnet(args.Magnet)
	if err != nil {
		return err
	}
	reply.Torrent = newTorrent(t)
	return nil
}

func newTorrent(t *Torrent) rainrpc.Torrent {
	return rainrpc.Torrent{
		ID:       t.ID(),
		Name:     t.Name(),
		InfoHash: t.InfoHash(),
		Port:     t.Port(),
	}
}

func (h *rpcHandler) RemoveTorrent(args *rainrpc.RemoveTorrentRequest, reply *rainrpc.RemoveTorrentResponse) error {
	h.session.RemoveTorrent(args.ID)
	return nil
}

func (h *rpcHandler) GetTorrentStats(args *rainrpc.GetTorrentStatsRequest, reply *rainrpc.GetTorrentStatsResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	reply.Stats = t.Stats()
	return nil
}

func (h *rpcHandler) GetTorrentTrackers(args *rainrpc.GetTorrentTrackersRequest, reply *rainrpc.GetTorrentTrackersResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	reply.Trackers = t.Trackers()
	return nil
}

func (h *rpcHandler) GetTorrentPeers(args *rainrpc.GetTorrentPeersRequest, reply *rainrpc.GetTorrentPeersResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	reply.Peers = t.Peers()
	return nil
}

func (h *rpcHandler) StartTorrent(args *rainrpc.StartTorrentRequest, reply *rainrpc.StartTorrentResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	t.Start()
	return nil
}

func (h *rpcHandler) StopTorrent(args *rainrpc.StopTorrentRequest, reply *rainrpc.StopTorrentResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	t.Stop()
	return nil
}

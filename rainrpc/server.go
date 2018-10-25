package rainrpc

import (
	"encoding/base64"
	"errors"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"strings"

	"github.com/cenkalti/rain/client"
)

type Server struct {
	rpcServer *rpc.Server
}

func NewServer(clt *client.Client) *Server {
	h := &handler{client: clt}
	srv := rpc.NewServer()
	srv.RegisterName("Client", h)
	srv.HandleHTTP(rpc.DefaultRPCPath, rpc.DefaultDebugPath)
	return &Server{rpcServer: srv}
}

func (s *Server) ListenAndServe(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.rpcServer.ServeCodec(jsonrpc.NewServerCodec(conn))
	}
}

type handler struct {
	client *client.Client
}

func (h *handler) ListTorrents(args *ListTorrentsRequest, reply *ListTorrentsResponse) error {
	torrents := h.client.ListTorrents()
	reply.Torrents = make([]Torrent, 0, len(torrents))
	for _, t := range torrents {
		reply.Torrents = append(reply.Torrents, newTorrent(t))
	}
	return nil
}

func (h *handler) AddTorrent(args *AddTorrentRequest, reply *AddTorrentResponse) error {
	r := base64.NewDecoder(base64.StdEncoding, strings.NewReader(args.Torrent))
	t, err := h.client.AddTorrent(r)
	if err != nil {
		return err
	}
	reply.Torrent = newTorrent(t)
	return nil
}

func (h *handler) AddMagnet(args *AddMagnetRequest, reply *AddTorrentResponse) error {
	t, err := h.client.AddMagnet(args.Magnet)
	if err != nil {
		return err
	}
	reply.Torrent = newTorrent(t)
	return nil
}

func newTorrent(t *client.Torrent) Torrent {
	return Torrent{
		ID:       t.ID,
		Name:     t.Name(),
		InfoHash: t.InfoHash(),
	}
}

func (h *handler) RemoveTorrent(args *RemoveTorrentRequest, reply *RemoveTorrentResponse) error {
	h.client.RemoveTorrent(args.ID)
	return nil
}

func (h *handler) GetTorrentStats(args *GetTorrentStatsRequest, reply *GetTorrentStatsResponse) error {
	t := h.client.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	reply.Stats = t.Stats()
	return nil
}

func (h *handler) StartTorrent(args *StartTorrentRequest, reply *StartTorrentResponse) error {
	t := h.client.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	t.Start()
	return nil
}

func (h *handler) StopTorrent(args *StopTorrentRequest, reply *StopTorrentResponse) error {
	t := h.client.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	t.Stop()
	return nil
}

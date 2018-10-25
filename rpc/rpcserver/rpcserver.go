package rpcserver

import (
	"encoding/base64"
	"errors"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"strings"

	"github.com/cenkalti/rain/client"
	"github.com/cenkalti/rain/rpc/rpctypes"
)

type RPCServer struct {
	rpcServer *rpc.Server
	addr      string
}

func New(clt *client.Client, addr string) *RPCServer {
	h := &handler{client: clt}
	srv := rpc.NewServer()
	srv.RegisterName("Client", h)
	srv.HandleHTTP(rpc.DefaultRPCPath, rpc.DefaultDebugPath)
	return &RPCServer{
		rpcServer: srv,
		addr:      addr,
	}
}

func (s *RPCServer) ListenAndServe() error {
	listener, err := net.Listen("tcp", s.addr)
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

func (h *handler) ListTorrents(args *rpctypes.ListTorrentsRequest, reply *rpctypes.ListTorrentsResponse) error {
	torrents := h.client.ListTorrents()
	reply.Torrents = make([]rpctypes.Torrent, 0, len(torrents))
	for _, t := range torrents {
		reply.Torrents = append(reply.Torrents, newTorrent(t))
	}
	return nil
}

func (h *handler) AddTorrent(args *rpctypes.AddTorrentRequest, reply *rpctypes.AddTorrentResponse) error {
	r := base64.NewDecoder(base64.StdEncoding, strings.NewReader(args.Torrent))
	t, err := h.client.AddTorrent(r)
	if err != nil {
		return err
	}
	reply.Torrent = newTorrent(t)
	return nil
}

func (h *handler) AddMagnet(args *rpctypes.AddMagnetRequest, reply *rpctypes.AddTorrentResponse) error {
	t, err := h.client.AddMagnet(args.Magnet)
	if err != nil {
		return err
	}
	reply.Torrent = newTorrent(t)
	return nil
}

func newTorrent(t *client.Torrent) rpctypes.Torrent {
	return rpctypes.Torrent{
		ID:       t.ID,
		Name:     t.Name(),
		InfoHash: t.InfoHash(),
	}
}

func (h *handler) RemoveTorrent(args *rpctypes.RemoveTorrentRequest, reply *rpctypes.RemoveTorrentResponse) error {
	h.client.RemoveTorrent(args.ID)
	return nil
}

func (h *handler) GetTorrentStats(args *rpctypes.GetTorrentStatsRequest, reply *rpctypes.GetTorrentStatsResponse) error {
	t := h.client.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	reply.Stats = t.Stats()
	return nil
}

func (h *handler) StartTorrent(args *rpctypes.StartTorrentRequest, reply *rpctypes.StartTorrentResponse) error {
	t := h.client.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	t.Start()
	return nil
}

func (h *handler) StopTorrent(args *rpctypes.StopTorrentRequest, reply *rpctypes.StopTorrentResponse) error {
	t := h.client.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	t.Stop()
	return nil
}

package rpcserver

import (
	"encoding/base64"
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
	reply.Torrents = make(map[uint64]rpctypes.Torrent)
	torrents := h.client.ListTorrents()
	for _, t := range torrents {
		reply.Torrents[t.ID] = rpctypes.Torrent{
			ID:       t.ID,
			Name:     t.Name,
			InfoHash: t.InfoHash,
		}
	}
	return nil
}

func (h *handler) AddTorrent(args *rpctypes.AddTorrentRequest, reply *rpctypes.AddTorrentResponse) error {
	r := base64.NewDecoder(base64.StdEncoding, strings.NewReader(args.Torrent))
	t, err := h.client.AddTorrent(r)
	if err != nil {
		return err
	}
	reply.Torrent = rpctypes.Torrent{ID: t.ID}
	return nil
}

func (h *handler) AddMagnet(args *rpctypes.AddMagnetRequest, reply *rpctypes.AddTorrentResponse) error {
	t, err := h.client.AddMagnet(args.Magnet)
	if err != nil {
		return err
	}
	reply.Torrent = rpctypes.Torrent{ID: t.ID}
	return nil
}

func (h *handler) RemoveTorrent(args *rpctypes.RemoveTorrentRequest, reply *rpctypes.RemoveTorrentResponse) error {
	h.client.Remove(args.ID)
	return nil
}

// func (h *handler) handleStart(w http.ResponseWriter, r *http.Request) {}
// func (h *handler) handleStop(w http.ResponseWriter, r *http.Request)  {}
// func (h *handler) handleStats(w http.ResponseWriter, r *http.Request) {}

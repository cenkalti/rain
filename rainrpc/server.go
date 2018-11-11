package rainrpc

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/rpc"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/rain/client"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/powerman/rpc-codec/jsonrpc2"
)

type Server struct {
	config     ServerConfig
	client     *client.Client
	rpcServer  *rpc.Server
	httpServer http.Server
	log        logger.Logger
}

type ServerConfig struct {
	Host            string
	Port            int
	ShutdownTimeout time.Duration
	Client          client.Config
}

var DefaultServerConfig = ServerConfig{
	Host:            "127.0.0.1",
	Port:            7246,
	ShutdownTimeout: 5 * time.Second,
	Client:          client.DefaultConfig,
}

func NewServer(cfg ServerConfig) (*Server, error) {
	clt, err := client.New(cfg.Client)
	if err != nil {
		return nil, err
	}
	h := &handler{client: clt}
	srv := rpc.NewServer()
	srv.RegisterName("Client", h)
	return &Server{
		config:    cfg,
		client:    clt,
		rpcServer: srv,
		httpServer: http.Server{
			Handler: jsonrpc2.HTTPHandler(srv),
		},
		log: logger.New("rpc server"),
	}, nil
}

func (s *Server) Start() error {
	addr := net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port))
	s.log.Infoln("RPC server is listening on", addr)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	go func() {
		err := s.httpServer.Serve(listener)
		if err == http.ErrServerClosed {
			return
		}
		s.log.Fatal(err)
	}()

	return nil
}

func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()
	err := s.httpServer.Shutdown(ctx)
	if err != nil {
		return err
	}
	s.client.Close()
	return nil
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

func (h *handler) AddMagnet(args *AddMagnetRequest, reply *AddMagnetResponse) error {
	t, err := h.client.AddMagnet(args.Magnet)
	if err != nil {
		return err
	}
	reply.Torrent = newTorrent(t)
	return nil
}

func newTorrent(t *client.Torrent) Torrent {
	return Torrent{
		ID:       t.ID(),
		Name:     t.Name(),
		InfoHash: t.InfoHash(),
		Port:     t.Port(),
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

func (h *handler) GetTorrentTrackers(args *GetTorrentTrackersRequest, reply *GetTorrentTrackersResponse) error {
	t := h.client.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	reply.Trackers = t.Trackers()
	return nil
}

func (h *handler) GetTorrentPeers(args *GetTorrentPeersRequest, reply *GetTorrentPeersResponse) error {
	t := h.client.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	reply.Peers = t.Peers()
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

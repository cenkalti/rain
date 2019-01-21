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

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/session"
	"github.com/powerman/rpc-codec/jsonrpc2"
)

type Server struct {
	config     ServerConfig
	session    *session.Session
	rpcServer  *rpc.Server
	httpServer http.Server
	log        logger.Logger
}

type ServerConfig struct {
	Host            string
	Port            int
	ShutdownTimeout time.Duration
	Session         session.Config
}

var DefaultServerConfig = ServerConfig{
	Host:            "127.0.0.1",
	Port:            7246,
	ShutdownTimeout: 5 * time.Second,
	Session:         session.DefaultConfig,
}

func NewServer(cfg ServerConfig) (*Server, error) {
	ses, err := session.New(cfg.Session)
	if err != nil {
		return nil, err
	}
	h := &handler{session: ses}
	srv := rpc.NewServer()
	srv.RegisterName("Client", h)
	return &Server{
		config:    cfg,
		session:   ses,
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
	s.session.Close()
	return nil
}

type handler struct {
	session *session.Session
}

func (h *handler) ListTorrents(args *ListTorrentsRequest, reply *ListTorrentsResponse) error {
	torrents := h.session.ListTorrents()
	reply.Torrents = make([]Torrent, 0, len(torrents))
	for _, t := range torrents {
		reply.Torrents = append(reply.Torrents, newTorrent(t))
	}
	return nil
}

func (h *handler) AddTorrent(args *AddTorrentRequest, reply *AddTorrentResponse) error {
	r := base64.NewDecoder(base64.StdEncoding, strings.NewReader(args.Torrent))
	t, err := h.session.AddTorrent(r)
	if err != nil {
		return err
	}
	reply.Torrent = newTorrent(t)
	return nil
}

func (h *handler) AddMagnet(args *AddMagnetRequest, reply *AddMagnetResponse) error {
	t, err := h.session.AddMagnet(args.Magnet)
	if err != nil {
		return err
	}
	reply.Torrent = newTorrent(t)
	return nil
}

func newTorrent(t *session.Torrent) Torrent {
	return Torrent{
		ID:       t.ID(),
		Name:     t.Name(),
		InfoHash: t.InfoHash(),
		Port:     t.Port(),
	}
}

func (h *handler) RemoveTorrent(args *RemoveTorrentRequest, reply *RemoveTorrentResponse) error {
	h.session.RemoveTorrent(args.ID)
	return nil
}

func (h *handler) GetTorrentStats(args *GetTorrentStatsRequest, reply *GetTorrentStatsResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	reply.Stats = t.Stats()
	return nil
}

func (h *handler) GetTorrentTrackers(args *GetTorrentTrackersRequest, reply *GetTorrentTrackersResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	reply.Trackers = t.Trackers()
	return nil
}

func (h *handler) GetTorrentPeers(args *GetTorrentPeersRequest, reply *GetTorrentPeersResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	reply.Peers = t.Peers()
	return nil
}

func (h *handler) StartTorrent(args *StartTorrentRequest, reply *StartTorrentResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	t.Start()
	return nil
}

func (h *handler) StopTorrent(args *StopTorrentRequest, reply *StopTorrentResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errors.New("torrent not found")
	}
	t.Stop()
	return nil
}

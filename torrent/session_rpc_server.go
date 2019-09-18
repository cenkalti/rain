package torrent

import (
	"context"
	"expvar"
	"net"
	"net/http"
	"net/rpc"
	"strconv"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/powerman/rpc-codec/jsonrpc2"
)

type rpcServer struct {
	rpcServer  *rpc.Server
	httpServer http.Server
	log        logger.Logger
}

func newRPCServer(ses *Session) *rpcServer {
	h := &rpcHandler{session: ses}
	srv := rpc.NewServer()
	_ = srv.RegisterName("Session", h)

	mux := http.NewServeMux()
	mux.Handle("/debug/vars", expvar.Handler())
	mux.HandleFunc("/move-torrent", h.handleMoveTorrent)
	mux.Handle("/", jsonrpc2.HTTPHandler(srv))

	return &rpcServer{
		rpcServer: srv,
		httpServer: http.Server{
			Handler: mux,
		},
		log: logger.New("rpc server"),
	}
}

func (s *rpcServer) Start(host string, port int) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	s.log.Infoln("RPC server is listening on", listener.Addr().String())

	go func() {
		err := s.httpServer.Serve(listener)
		if err == http.ErrServerClosed {
			return
		}
		s.log.Fatal(err)
	}()

	return nil
}

func (s *rpcServer) Stop(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

package session

import (
	"context"
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
	srv.RegisterName("Session", h)
	return &rpcServer{
		rpcServer: srv,
		httpServer: http.Server{
			Handler: jsonrpc2.HTTPHandler(srv),
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

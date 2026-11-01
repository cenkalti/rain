package torrent

import (
	"context"
	"expvar"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/creachadair/jrpc2/jhttp"
)

type rpcServer struct {
	bridge     jhttp.Bridge
	httpServer http.Server
	log        logger.Logger
}

func newRPCServer(ses *Session) *rpcServer {
	h := &rpcHandler{session: ses}
	bridge := jhttp.NewBridge(h.methods(), nil)

	mux := http.NewServeMux()
	mux.Handle("/debug/vars", expvar.Handler())
	mux.HandleFunc("/move-torrent", h.handleMoveTorrent)
	mux.Handle("/", bridge)

	return &rpcServer{
		bridge: bridge,
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
		// Log instead of Fatal: the torrent package is embeddable as a
		// library, so an unexpected Serve error must not os.Exit the host
		// process. (The pprof serve goroutine in main.go logs likewise.)
		s.log.Errorln("rpc server stopped serving:", err)
	}()

	return nil
}

func (s *rpcServer) Stop(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := s.httpServer.Shutdown(ctx)
	_ = s.bridge.Close()
	return err
}

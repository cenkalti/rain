package rpcserver

import (
	"net/http"

	"github.com/cenkalti/rain/client"
)

type RPCServer struct {
	http.Server
}

func New(clt *client.Client, addr string) *RPCServer {
	return &RPCServer{
		Server: http.Server{
			Addr:    addr,
			Handler: NewHandler(clt),
		},
	}
}

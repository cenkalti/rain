package rpcserver

import (
	"encoding/json"
	"net/http"

	"github.com/cenkalti/rain/client"
	"github.com/cenkalti/rain/rpc/rpctypes"
)

type RPCServer struct {
	http.Server
}

func New(clt *client.Client, addr string) *RPCServer {
	return &RPCServer{
		Server: http.Server{
			Addr:    addr,
			Handler: newHandler(clt),
		},
	}
}

type handler struct {
	*http.ServeMux
	client *client.Client
}

func newHandler(c *client.Client) *handler {
	h := &handler{
		ServeMux: http.NewServeMux(),
		client:   c,
	}
	h.HandleFunc("/list", h.handleList)
	h.HandleFunc("/add", h.handleAdd)
	h.HandleFunc("/remove", h.handleRemove)
	h.HandleFunc("/start", h.handleStart)
	h.HandleFunc("/stop", h.handleStop)
	h.HandleFunc("/stats", h.handleStats)
	return h
}

func (h *handler) handleList(w http.ResponseWriter, r *http.Request) {
	resp := rpctypes.ListTorrentsResponse{
		Torrents: make(map[uint64]rpctypes.Torrent),
	}
	torrents := h.client.ListTorrents()
	for _, t := range torrents {
		resp.Torrents[t.ID] = rpctypes.Torrent{ID: t.ID}
	}
	enc := json.NewEncoder(w)
	enc.Encode(resp)
}

func (h *handler) handleAdd(w http.ResponseWriter, r *http.Request)    {}
func (h *handler) handleRemove(w http.ResponseWriter, r *http.Request) {}
func (h *handler) handleStart(w http.ResponseWriter, r *http.Request)  {}
func (h *handler) handleStop(w http.ResponseWriter, r *http.Request)   {}
func (h *handler) handleStats(w http.ResponseWriter, r *http.Request)  {}

package rpcserver

import (
	"encoding/json"
	"net/http"

	"github.com/cenkalti/rain/client"
	"github.com/cenkalti/rain/rpc/rpctypes"
)

type Handler struct {
	*http.ServeMux
	client *client.Client
}

func NewHandler(c *client.Client) *Handler {
	h := &Handler{
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

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) handleAdd(w http.ResponseWriter, r *http.Request)    {}
func (h *Handler) handleRemove(w http.ResponseWriter, r *http.Request) {}
func (h *Handler) handleStart(w http.ResponseWriter, r *http.Request)  {}
func (h *Handler) handleStop(w http.ResponseWriter, r *http.Request)   {}
func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request)  {}

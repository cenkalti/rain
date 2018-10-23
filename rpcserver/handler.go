package rpcserver

import (
	"encoding/json"
	"net/http"

	"github.com/cenkalti/rain/client"
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
	torrents := h.client.ListTorrents()
	enc := json.NewEncoder(w)
	enc.Encode(torrents)
}

func (h *Handler) handleAdd(w http.ResponseWriter, r *http.Request)    {}
func (h *Handler) handleRemove(w http.ResponseWriter, r *http.Request) {}
func (h *Handler) handleStart(w http.ResponseWriter, r *http.Request)  {}
func (h *Handler) handleStop(w http.ResponseWriter, r *http.Request)   {}
func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request)  {}

type Torrent struct {
	ID int
}

func NewTorrent(t *client.Torrent) *Torrent {
	return &Torrent{
		ID: t.ID,
	}
}

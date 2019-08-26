// Package rainrpc provides a RPC client implementation for communicating with Rain session.
package rainrpc

import (
	"encoding/base64"
	"io"
	"io/ioutil"

	"github.com/cenkalti/rain/internal/rpctypes"
	"github.com/powerman/rpc-codec/jsonrpc2"
)

type Client struct {
	client *jsonrpc2.Client
}

func NewClient(addr string) *Client {
	return &Client{client: jsonrpc2.NewHTTPClient(addr)}
}

func (c *Client) Close() error {
	return c.client.Close()
}

func (c *Client) ServerVersion() (string, error) {
	var reply string
	return reply, c.client.Call("Session.Version", nil, &reply)
}

func (c *Client) ListTorrents() ([]rpctypes.Torrent, error) {
	var reply rpctypes.ListTorrentsResponse
	return reply.Torrents, c.client.Call("Session.ListTorrents", nil, &reply)
}

type AddTorrentOptions struct {
	Stopped bool
	ID      string
}

func (c *Client) AddTorrent(f io.Reader, options *AddTorrentOptions) (*rpctypes.Torrent, error) {
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}
	args := rpctypes.AddTorrentRequest{Torrent: base64.StdEncoding.EncodeToString(b)}
	if options != nil {
		args.AddTorrentOptions.Stopped = options.Stopped
		args.AddTorrentOptions.ID = options.ID
	}
	var reply rpctypes.AddTorrentResponse
	return &reply.Torrent, c.client.Call("Session.AddTorrent", args, &reply)
}

func (c *Client) AddURI(uri string, options *AddTorrentOptions) (*rpctypes.Torrent, error) {
	args := rpctypes.AddURIRequest{URI: uri}
	if options != nil {
		args.AddTorrentOptions.Stopped = options.Stopped
	}
	var reply rpctypes.AddURIResponse
	return &reply.Torrent, c.client.Call("Session.AddURI", args, &reply)
}

func (c *Client) RemoveTorrent(id string) error {
	args := rpctypes.RemoveTorrentRequest{ID: id}
	var reply rpctypes.RemoveTorrentResponse
	return c.client.Call("Session.RemoveTorrent", args, &reply)
}

func (c *Client) GetTorrentStats(id string) (*rpctypes.Stats, error) {
	args := rpctypes.GetTorrentStatsRequest{ID: id}
	var reply rpctypes.GetTorrentStatsResponse
	return &reply.Stats, c.client.Call("Session.GetTorrentStats", args, &reply)
}

func (c *Client) GetSessionStats() (*rpctypes.SessionStats, error) {
	args := rpctypes.GetSessionStatsRequest{}
	var reply rpctypes.GetSessionStatsResponse
	return &reply.Stats, c.client.Call("Session.GetSessionStats", args, &reply)
}

func (c *Client) GetTorrentTrackers(id string) ([]rpctypes.Tracker, error) {
	args := rpctypes.GetTorrentTrackersRequest{ID: id}
	var reply rpctypes.GetTorrentTrackersResponse
	return reply.Trackers, c.client.Call("Session.GetTorrentTrackers", args, &reply)
}

func (c *Client) GetTorrentPeers(id string) ([]rpctypes.Peer, error) {
	args := rpctypes.GetTorrentPeersRequest{ID: id}
	var reply rpctypes.GetTorrentPeersResponse
	return reply.Peers, c.client.Call("Session.GetTorrentPeers", args, &reply)
}

func (c *Client) GetTorrentWebseeds(id string) ([]rpctypes.Webseed, error) {
	args := rpctypes.GetTorrentWebseedsRequest{ID: id}
	var reply rpctypes.GetTorrentWebseedsResponse
	return reply.Webseeds, c.client.Call("Session.GetTorrentWebseeds", args, &reply)
}

func (c *Client) StartTorrent(id string) error {
	args := rpctypes.StartTorrentRequest{ID: id}
	var reply rpctypes.StartTorrentResponse
	return c.client.Call("Session.StartTorrent", args, &reply)
}

func (c *Client) StopTorrent(id string) error {
	args := rpctypes.StopTorrentRequest{ID: id}
	var reply rpctypes.StopTorrentResponse
	return c.client.Call("Session.StopTorrent", args, &reply)
}

func (c *Client) StartAllTorrents() error {
	args := rpctypes.StartAllTorrentsRequest{}
	var reply rpctypes.StartAllTorrentsResponse
	return c.client.Call("Session.StartAllTorrents", args, &reply)
}

func (c *Client) StopAllTorrents() error {
	args := rpctypes.StopAllTorrentsRequest{}
	var reply rpctypes.StopAllTorrentsResponse
	return c.client.Call("Session.StopAllTorrents", args, &reply)
}

func (c *Client) AddPeer(id string, addr string) error {
	args := rpctypes.AddPeerRequest{ID: id, Addr: addr}
	var reply rpctypes.AddPeerResponse
	return c.client.Call("Session.AddPeer", args, &reply)
}

func (c *Client) AddTracker(id string, uri string) error {
	args := rpctypes.AddTrackerRequest{ID: id, URL: uri}
	var reply rpctypes.AddTrackerResponse
	return c.client.Call("Session.AddTracker", args, &reply)
}

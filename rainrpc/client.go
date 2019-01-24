package rainrpc

import (
	"encoding/base64"
	"io"
	"io/ioutil"

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

func (c *Client) ListTorrents() ([]Torrent, error) {
	var reply ListTorrentsResponse
	return reply.Torrents, c.client.Call("Session.ListTorrents", nil, &reply)
}

func (c *Client) AddTorrent(f io.Reader) (*Torrent, error) {
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}
	args := AddTorrentRequest{Torrent: base64.StdEncoding.EncodeToString(b)}
	var reply AddTorrentResponse
	return &reply.Torrent, c.client.Call("Session.AddTorrent", args, &reply)
}

func (c *Client) AddURI(uri string) (*Torrent, error) {
	args := AddURIRequest{URI: uri}
	var reply AddURIResponse
	return &reply.Torrent, c.client.Call("Session.AddURI", args, &reply)
}

func (c *Client) RemoveTorrent(id uint64) error {
	args := RemoveTorrentRequest{ID: id}
	var reply RemoveTorrentResponse
	return c.client.Call("Session.RemoveTorrent", args, &reply)
}

func (c *Client) GetTorrentStats(id uint64) (*Stats, error) {
	args := GetTorrentStatsRequest{ID: id}
	var reply GetTorrentStatsResponse
	return &reply.Stats, c.client.Call("Session.GetTorrentStats", args, &reply)
}

func (c *Client) GetTorrentTrackers(id uint64) ([]Tracker, error) {
	args := GetTorrentTrackersRequest{ID: id}
	var reply GetTorrentTrackersResponse
	return reply.Trackers, c.client.Call("Session.GetTorrentTrackers", args, &reply)
}

func (c *Client) GetTorrentPeers(id uint64) ([]Peer, error) {
	args := GetTorrentPeersRequest{ID: id}
	var reply GetTorrentPeersResponse
	return reply.Peers, c.client.Call("Session.GetTorrentPeers", args, &reply)
}

func (c *Client) StartTorrent(id uint64) error {
	args := StartTorrentRequest{ID: id}
	var reply StartTorrentResponse
	return c.client.Call("Session.StartTorrent", args, &reply)
}

func (c *Client) StopTorrent(id uint64) error {
	args := StopTorrentRequest{ID: id}
	var reply StopTorrentResponse
	return c.client.Call("Session.StopTorrent", args, &reply)
}

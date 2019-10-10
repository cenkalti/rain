// Package rainrpc provides a RPC client implementation for communicating with Rain session.
package rainrpc

import (
	"encoding/base64"
	"io"
	"io/ioutil"

	"github.com/cenkalti/rain/internal/rpctypes"
	"github.com/powerman/rpc-codec/jsonrpc2"
)

// Client is a JSON-RPC 2.0 client for calling methods of a remote Session.
type Client struct {
	client *jsonrpc2.Client
	addr   string
}

// NewClient returns a new Client for remote address.
func NewClient(addr string) *Client {
	return &Client{
		client: jsonrpc2.NewHTTPClient(addr),
		addr:   addr,
	}
}

// Addr returns the address of remote Session.
func (c *Client) Addr() string {
	return c.addr
}

// Close the client.
func (c *Client) Close() error {
	return c.client.Close()
}

// ServerVersion returns the Rain version on remote server.
func (c *Client) ServerVersion() (string, error) {
	var reply string
	return reply, c.client.Call("Session.Version", nil, &reply)
}

// ListTorrents returns the list of torrents in remote Session.
func (c *Client) ListTorrents() ([]rpctypes.Torrent, error) {
	var reply rpctypes.ListTorrentsResponse
	return reply.Torrents, c.client.Call("Session.ListTorrents", nil, &reply)
}

// AddTorrentOptions contains optional parameters for adding a new Torrent.
type AddTorrentOptions struct {
	ID                string
	Stopped           bool
	StopAfterDownload bool
}

// AddTorrent adds a new torrent by reading .torrent file.
func (c *Client) AddTorrent(f io.Reader, options *AddTorrentOptions) (*rpctypes.Torrent, error) {
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}
	args := rpctypes.AddTorrentRequest{Torrent: base64.StdEncoding.EncodeToString(b)}
	if options != nil {
		args.AddTorrentOptions.ID = options.ID
		args.AddTorrentOptions.Stopped = options.Stopped
		args.AddTorrentOptions.StopAfterDownload = options.StopAfterDownload
	}
	var reply rpctypes.AddTorrentResponse
	return &reply.Torrent, c.client.Call("Session.AddTorrent", args, &reply)
}

// AddURI adds a new torrent from a HTTP or magnet link.
func (c *Client) AddURI(uri string, options *AddTorrentOptions) (*rpctypes.Torrent, error) {
	args := rpctypes.AddURIRequest{URI: uri}
	if options != nil {
		args.AddTorrentOptions.ID = options.ID
		args.AddTorrentOptions.Stopped = options.Stopped
		args.AddTorrentOptions.StopAfterDownload = options.StopAfterDownload
	}
	var reply rpctypes.AddURIResponse
	return &reply.Torrent, c.client.Call("Session.AddURI", args, &reply)
}

// RemoveTorrent removes a torrent from remote Session and deletes its data.
func (c *Client) RemoveTorrent(id string) error {
	args := rpctypes.RemoveTorrentRequest{ID: id}
	var reply rpctypes.RemoveTorrentResponse
	return c.client.Call("Session.RemoveTorrent", args, &reply)
}

// CleanDatabase removes invalid records in session database.
func (c *Client) CleanDatabase() error {
	var args rpctypes.CleanDatabaseRequest
	var reply rpctypes.CleanDatabaseResponse
	return c.client.Call("Session.CleanDatabase", args, &reply)
}

// GetTorrentStats returns statistics about a torrent.
func (c *Client) GetTorrentStats(id string) (*rpctypes.Stats, error) {
	args := rpctypes.GetTorrentStatsRequest{ID: id}
	var reply rpctypes.GetTorrentStatsResponse
	return &reply.Stats, c.client.Call("Session.GetTorrentStats", args, &reply)
}

// GetSessionStats returns statistics about the remote Session.
func (c *Client) GetSessionStats() (*rpctypes.SessionStats, error) {
	args := rpctypes.GetSessionStatsRequest{}
	var reply rpctypes.GetSessionStatsResponse
	return &reply.Stats, c.client.Call("Session.GetSessionStats", args, &reply)
}

// GetMagnet returns the torrent as a magnet link.
func (c *Client) GetMagnet(id string) (string, error) {
	args := rpctypes.GetMagnetRequest{ID: id}
	var reply rpctypes.GetMagnetResponse
	err := c.client.Call("Session.GetMagnet", args, &reply)
	return reply.Magnet, err
}

// GetTorrent returns the bytes of a .torrent file.
func (c *Client) GetTorrent(id string) ([]byte, error) {
	args := rpctypes.GetTorrentRequest{ID: id}
	var reply rpctypes.GetTorrentResponse
	err := c.client.Call("Session.GetTorrent", args, &reply)
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(reply.Torrent)
}

// GetTorrentTrackers returns the list of tracker in the torrent.
func (c *Client) GetTorrentTrackers(id string) ([]rpctypes.Tracker, error) {
	args := rpctypes.GetTorrentTrackersRequest{ID: id}
	var reply rpctypes.GetTorrentTrackersResponse
	return reply.Trackers, c.client.Call("Session.GetTorrentTrackers", args, &reply)
}

// GetTorrentPeers returns the list of connected peers of a torrent.
func (c *Client) GetTorrentPeers(id string) ([]rpctypes.Peer, error) {
	args := rpctypes.GetTorrentPeersRequest{ID: id}
	var reply rpctypes.GetTorrentPeersResponse
	return reply.Peers, c.client.Call("Session.GetTorrentPeers", args, &reply)
}

// GetTorrentWebseeds returns the WebSeed sources of a torrent.
func (c *Client) GetTorrentWebseeds(id string) ([]rpctypes.Webseed, error) {
	args := rpctypes.GetTorrentWebseedsRequest{ID: id}
	var reply rpctypes.GetTorrentWebseedsResponse
	return reply.Webseeds, c.client.Call("Session.GetTorrentWebseeds", args, &reply)
}

// StartTorrent starts the torrent.
func (c *Client) StartTorrent(id string) error {
	args := rpctypes.StartTorrentRequest{ID: id}
	var reply rpctypes.StartTorrentResponse
	return c.client.Call("Session.StartTorrent", args, &reply)
}

// StopTorrent stops the torrent.
func (c *Client) StopTorrent(id string) error {
	args := rpctypes.StopTorrentRequest{ID: id}
	var reply rpctypes.StopTorrentResponse
	return c.client.Call("Session.StopTorrent", args, &reply)
}

// AnnounceTorrent forces the torrent to re-announce to trackers and DHT.
func (c *Client) AnnounceTorrent(id string) error {
	args := rpctypes.AnnounceTorrentRequest{ID: id}
	var reply rpctypes.AnnounceTorrentResponse
	return c.client.Call("Session.AnnounceTorrent", args, &reply)
}

// VerifyTorrent stops the torrent and verifies all of the pieces on disk.
// After verification is done, the torrent stays in stopped state.
func (c *Client) VerifyTorrent(id string) error {
	args := rpctypes.VerifyTorrentRequest{ID: id}
	var reply rpctypes.VerifyTorrentResponse
	return c.client.Call("Session.VerifyTorrent", args, &reply)
}

// MoveTorrent moves the torrent to another Session.
func (c *Client) MoveTorrent(id, target string) error {
	args := rpctypes.MoveTorrentRequest{ID: id, Target: target}
	var reply rpctypes.MoveTorrentResponse
	return c.client.Call("Session.MoveTorrent", args, &reply)
}

// StartAllTorrents starts all torrents in the Session.
func (c *Client) StartAllTorrents() error {
	args := rpctypes.StartAllTorrentsRequest{}
	var reply rpctypes.StartAllTorrentsResponse
	return c.client.Call("Session.StartAllTorrents", args, &reply)
}

// StopAllTorrents stops all the torrents in the session.
func (c *Client) StopAllTorrents() error {
	args := rpctypes.StopAllTorrentsRequest{}
	var reply rpctypes.StopAllTorrentsResponse
	return c.client.Call("Session.StopAllTorrents", args, &reply)
}

// AddPeer adds a new peer the a torrent.
func (c *Client) AddPeer(id string, addr string) error {
	args := rpctypes.AddPeerRequest{ID: id, Addr: addr}
	var reply rpctypes.AddPeerResponse
	return c.client.Call("Session.AddPeer", args, &reply)
}

// AddTracker adds a new tracker to a torrent.
func (c *Client) AddTracker(id string, uri string) error {
	args := rpctypes.AddTrackerRequest{ID: id, URL: uri}
	var reply rpctypes.AddTrackerResponse
	return c.client.Call("Session.AddTracker", args, &reply)
}

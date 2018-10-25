package rainrpc

import (
	"encoding/base64"
	"io"
	"io/ioutil"
	"net/rpc"
	"net/rpc/jsonrpc"
)

type Client struct {
	client *rpc.Client
}

func NewClient(addr string) (*Client, error) {
	clt, err := jsonrpc.Dial("tcp", addr)
	return &Client{client: clt}, err
}

func (c *Client) Close() error {
	return c.client.Close()
}

func (c *Client) ListTorrents() (*ListTorrentsResponse, error) {
	var reply ListTorrentsResponse
	return &reply, c.client.Call("Client.ListTorrents", nil, &reply)
}

func (c *Client) AddTorrent(f io.Reader) (*AddTorrentResponse, error) {
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}
	args := AddTorrentRequest{Torrent: base64.StdEncoding.EncodeToString(b)}
	var reply AddTorrentResponse
	return &reply, c.client.Call("Client.AddTorrent", args, &reply)
}

func (c *Client) AddMagnet(magnet string) (*AddMagnetResponse, error) {
	args := AddMagnetRequest{Magnet: magnet}
	var reply AddMagnetResponse
	return &reply, c.client.Call("Client.AddMagnet", args, &reply)
}

func (c *Client) RemoveTorrent(id uint64) (*RemoveTorrentResponse, error) {
	args := RemoveTorrentRequest{ID: id}
	var reply RemoveTorrentResponse
	return &reply, c.client.Call("Client.RemoveTorrent", args, &reply)
}

func (c *Client) GetTorrentStats(id uint64) (*GetTorrentStatsResponse, error) {
	args := GetTorrentStatsRequest{ID: id}
	var reply GetTorrentStatsResponse
	return &reply, c.client.Call("Client.GetTorrentStats", args, &reply)
}

func (c *Client) StartTorrent(id uint64) (*StartTorrentResponse, error) {
	args := StartTorrentRequest{ID: id}
	var reply StartTorrentResponse
	return &reply, c.client.Call("Client.StartTorrent", args, &reply)
}

func (c *Client) StopTorrent(id uint64) (*StopTorrentResponse, error) {
	args := StopTorrentRequest{ID: id}
	var reply StopTorrentResponse
	return &reply, c.client.Call("Client.StopTorrent", args, &reply)
}

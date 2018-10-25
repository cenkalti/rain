package rpcclient

import (
	"encoding/base64"
	"io"
	"io/ioutil"
	"net/rpc"
	"net/rpc/jsonrpc"

	"github.com/cenkalti/rain/rpc/rpctypes"
)

type RPCClient struct {
	client *rpc.Client
}

func New(addr string) (*RPCClient, error) {
	clt, err := jsonrpc.Dial("tcp", addr)
	return &RPCClient{client: clt}, err
}

func (c *RPCClient) Close() error {
	return c.client.Close()
}

func (c *RPCClient) ListTorrents() (*rpctypes.ListTorrentsResponse, error) {
	var reply rpctypes.ListTorrentsResponse
	return &reply, c.client.Call("Client.ListTorrents", nil, &reply)
}

func (c *RPCClient) AddTorrent(f io.Reader) (*rpctypes.AddTorrentResponse, error) {
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}
	args := rpctypes.AddTorrentRequest{Torrent: base64.StdEncoding.EncodeToString(b)}
	var reply rpctypes.AddTorrentResponse
	return &reply, c.client.Call("Client.AddTorrent", args, &reply)
}

func (c *RPCClient) AddMagnet(magnet string) (*rpctypes.AddTorrentResponse, error) {
	args := rpctypes.AddMagnetRequest{Magnet: magnet}
	var reply rpctypes.AddTorrentResponse
	return &reply, c.client.Call("Client.AddMagnet", args, &reply)
}

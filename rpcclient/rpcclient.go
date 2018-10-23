package rpcclient

type RPCClient struct {
	url string
}

func New(url string) *RPCClient {
	return &RPCClient{url: url}
}

func (c *RPCClient) ListTorrents() ([]Torrent, error) {
	return nil, nil
}

type Torrent struct{}

package rpcclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/cenkalti/rain/rpc/rpctypes"
)

type RPCClient struct {
	url    string
	client http.Client
}

func New(url string) *RPCClient {
	return &RPCClient{url: url}
}

func (c *RPCClient) ListTorrents() (rpctypes.ListTorrentsResponse, error) {
	var resp rpctypes.ListTorrentsResponse
	err := c.request(http.MethodGet, "/list", nil, &resp)
	return resp, err
}

func (c *RPCClient) endpoint(path string) string {
	return c.url + path
}

func (c *RPCClient) request(method, path string, req, resp interface{}) error {
	var body io.Reader
	if req != nil {
		b, err := json.Marshal(&req)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	httpReq, err := http.NewRequest(method, c.endpoint(path), body)
	if err != nil {
		return err
	}
	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return err
	}
	if httpResp.StatusCode != 200 {
		return fmt.Errorf("rpc error (status=%d)", httpResp.StatusCode)
	}
	defer httpResp.Body.Close()
	return json.NewDecoder(httpResp.Body).Decode(&resp)
}

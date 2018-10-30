package main

import (
	"io/ioutil"

	"github.com/cenkalti/rain/client"
	"github.com/cenkalti/rain/rainrpc"
	// "github.com/cenkalti/rain/torrent"
	"gopkg.in/yaml.v2"
)

type Config struct {
	RPCServer rainrpc.ServerConfig
	Client    client.Config
	// Torrent *client.Config
}

var DefaultConfig = Config{
	RPCServer: rainrpc.DefaultServerConfig,
	Client:    client.DefaultConfig,
	// Torrent: torrent.Config,
}

// LoadFile loads config values in a YAML file.
func (c *Config) LoadFile(filename string) error {
	b, err := ioutil.ReadFile(filename) // nolint: gosec
	if err != nil {
		return err
	}
	return yaml.Unmarshal(b, c)
}

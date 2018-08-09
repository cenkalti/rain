package client

import (
	"github.com/cenkalti/rain/logger"
)

type Client struct {
	config *Config
	log    logger.Logger
}

// NewClient returns a pointer to new Rain BitTorrent client.
func NewClient(c *Config) (*Client, error) {
	if c == nil {
		c = NewConfig()
	}
	return &Client{
		config: c,
		log:    logger.New("client"),
	}, nil
}

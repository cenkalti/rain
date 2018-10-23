package client

import (
	"io/ioutil"

	"gopkg.in/yaml.v2"
)

// Config for Client.
type Config struct {
	Database string
}

var defaultConfig = Config{
	Database: "rain.db",
}

// NewConfig returns a default Config.
func NewConfig() *Config {
	c := defaultConfig
	return &c
}

// LoadFile loads config values in a YAML file.
func (c *Config) LoadFile(filename string) error {
	b, err := ioutil.ReadFile(filename) // nolint: gosec
	if err != nil {
		return err
	}
	return yaml.Unmarshal(b, c)
}

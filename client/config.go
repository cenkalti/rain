package client

import (
	"io/ioutil"

	"gopkg.in/yaml.v2"
)

// Config for Client.
type Config struct {
	// Database file to save resume data.
	Database string
	// DataDir is where files are downloaded.
	DataDir string
	// PortBegin, PortEnd int
}

var DefaultConfig = Config{
	Database: "~/.rain/resume.db",
	DataDir:  "~/rain-downloads",
	// PortBegin: 50000,
	// PortEnd:   60000,
}

// NewConfig returns new Config that is initialized with values from DefaultConfig.
func NewConfig() *Config {
	c := DefaultConfig
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

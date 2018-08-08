package rain

import (
	"io/ioutil"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Port       int
	Encryption struct {
		DisableOutgoing bool `yaml:"disable_outgoing"`
		ForceOutgoing   bool `yaml:"force_outgoing"`
		ForceIncoming   bool `yaml:"force_incoming"`
	}
}

var defaultConfig = Config{
	Port: 6881,
}

func NewConfig() *Config {
	var c Config
	c = defaultConfig
	return &c
}

func (c *Config) LoadFile(filename string) error {
	b, err := ioutil.ReadFile(filename) // nolint: gosec
	if err != nil {
		return err
	}
	return yaml.Unmarshal(b, c)
}

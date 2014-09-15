package rain

import (
	"io/ioutil"
	"os"

	"gopkg.in/yaml.v1"
)

type Config struct {
	filename                  string
	Port                      uint16
	DisableOutgoingEncryption bool
	ForceOutgoingEncryption   bool
	ForceIncomingEncryption   bool
}

var DefaultConfig = Config{
	Port: 6881,
}

func LoadConfig(filename string) (*Config, error) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			c := DefaultConfig
			c.filename = filename
			return &c, c.Save()
		} else {
			return nil, err
		}
	}
	c := &Config{filename: filename}
	err = yaml.Unmarshal(b, c)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) Save() error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(c.filename, b, 0644)
}

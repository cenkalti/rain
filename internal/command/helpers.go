// Package command implements the individual rain CLI commands.
//
// Each command is built by an exported XCommand function (DownloadCommand,
// ServerCommand, ClientCommand, ...) that returns a cli.Command. The
// application wiring — global flags, profiling hooks and dispatch — lives in
// the root main package, which assembles these commands and runs them.
package command

import (
	"os"
	"strings"

	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/cenkalti/rain/v2/torrent"
	"github.com/urfave/cli"
	"gopkg.in/yaml.v2"
)

var log = logger.New("rain")

func prepareConfig(c *cli.Context) (torrent.Config, error) {
	cfg := torrent.DefaultConfig

	configPath := c.String("config")
	if configPath != "" {
		cp := os.ExpandEnv(configPath)
		b, err := os.ReadFile(cp)
		switch {
		case os.IsNotExist(err):
			if c.IsSet("config") {
				return cfg, err
			}
			log.Noticef("config file not found at %q, using default config", cp)
		case err != nil:
			return cfg, err
		default:
			err = yaml.Unmarshal(b, &cfg)
			if err != nil {
				return cfg, err
			}
			log.Infoln("config loaded from:", cp)
			b, err = yaml.Marshal(&cfg)
			if err != nil {
				return cfg, err
			}
			log.Debug("\n" + string(b))
		}
	}
	return cfg, nil
}

// isURI reports whether arg is a magnet link or an HTTP(S) URL rather than a
// local file path.
func isURI(arg string) bool {
	return strings.HasPrefix(arg, "magnet:") || strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://")
}

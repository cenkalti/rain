package command

import (
	"github.com/br0xen/boltbrowser/pkg/boltbrowser"
	"github.com/urfave/cli"
)

func BoltBrowserCommand() cli.Command {
	return cli.Command{
		Name:   "boltbrowser",
		Hidden: true,
		Action: handleBoltBrowser,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "file,f",
				Required: true,
			},
		},
	}
}

func handleBoltBrowser(c *cli.Context) error {
	args := boltbrowser.DefaultArgs()
	files := []string{c.String("file")}
	return boltbrowser.Main(args, files)
}

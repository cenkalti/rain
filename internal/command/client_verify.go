package command

import "github.com/urfave/cli"

func clientVerifyCommand() cli.Command {
	return cli.Command{
		Name:     "verify",
		Usage:    "verify files",
		Category: "Actions",
		Action:   handleVerify,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
		},
	}
}

func handleVerify(c *cli.Context) error {
	return clt.VerifyTorrent(c.String("id"))
}

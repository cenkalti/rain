package command

import (
	"fmt"

	"github.com/urfave/cli"
)

func clientMagnetCommand() cli.Command {
	return cli.Command{
		Name:     "magnet",
		Usage:    "get magnet link",
		Category: "Getters",
		Action:   handleGetMagnet,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
		},
	}
}

func handleGetMagnet(c *cli.Context) error {
	magnet, err := clt.GetMagnet(c.String("id"))
	if err != nil {
		return err
	}
	fmt.Println(magnet)
	return nil
}

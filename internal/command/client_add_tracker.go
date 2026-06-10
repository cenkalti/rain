package command

import "github.com/urfave/cli"

func clientAddTrackerCommand() cli.Command {
	return cli.Command{
		Name:     "add-tracker",
		Usage:    "add tracker to torrent",
		Category: "Actions",
		Action:   handleAddTracker,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:     "id",
				Required: true,
			},
			cli.StringFlag{
				Name:     "tracker,t",
				Required: true,
				Usage:    "tracker URL",
			},
		},
	}
}

func handleAddTracker(c *cli.Context) error {
	return clt.AddTracker(c.String("id"), c.String("tracker"))
}

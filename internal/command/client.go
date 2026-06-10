package command

import (
	"os"
	"strconv"
	"time"

	"github.com/cenkalti/rain/v2/rainrpc"
	"github.com/cenkalti/rain/v2/torrent"
	"github.com/hokaccha/go-prettyjson"
	"github.com/urfave/cli"
)

// clt is the RPC client used by all client subcommands. It is initialized in
// handleBeforeClient before any subcommand action runs.
var clt *rainrpc.Client

func ClientCommand() cli.Command {
	return cli.Command{
		Name:  "client",
		Usage: "send rpc request to server",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "url",
				Usage: "URL of RPC server",
				Value: "http://127.0.0.1:" + strconv.Itoa(torrent.DefaultConfig.RPCPort),
			},
			cli.DurationFlag{
				Name:  "timeout",
				Usage: "request timeout",
				Value: 10 * time.Second,
			},
		},
		Before: handleBeforeClient,
		Subcommands: []cli.Command{
			clientVersionCommand(),
			clientListCommand(),
			clientAddCommand(),
			clientRemoveCommand(),
			clientCleanDatabaseCommand(),
			clientStatsCommand(),
			clientSessionStatsCommand(),
			clientTrackersCommand(),
			clientWebseedsCommand(),
			clientFilesCommand(),
			clientFileStatsCommand(),
			clientPeersCommand(),
			clientAddPeerCommand(),
			clientAddTrackerCommand(),
			clientAnnounceCommand(),
			clientVerifyCommand(),
			clientStartCommand(),
			clientStopCommand(),
			clientStartAllCommand(),
			clientStopAllCommand(),
			clientMoveCommand(),
			clientSaveTorrentCommand(),
			clientMagnetCommand(),
			clientConsoleCommand(),
		},
	}
}

func handleBeforeClient(c *cli.Context) error {
	clt = rainrpc.NewClient(c.String("url"))
	clt.SetTimeout(c.Duration("timeout"))
	return nil
}

// printJSON writes v to stdout as colorized, compact JSON followed by a
// newline. Used by the client subcommands that dump RPC responses.
func printJSON(v any) error {
	b, err := prettyjson.Marshal(v)
	if err != nil {
		return err
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

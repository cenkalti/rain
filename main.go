package main

import (
	"io/ioutil"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/boltbrowser/boltbrowser"
	clog "github.com/cenkalti/log"
	"github.com/cenkalti/rain/internal/clientversion"
	"github.com/cenkalti/rain/internal/console"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/rainrpc"
	"github.com/cenkalti/rain/session"
	"github.com/hokaccha/go-prettyjson"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli"
	"gopkg.in/yaml.v2"
)

var (
	app = cli.NewApp()
	clt *rainrpc.Client
	log = logger.New("rain")
)

func main() {
	app.Version = clientversion.Version
	app.Usage = "BitTorrent client"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "config, c",
			Usage: "read config from `FILE`",
		},
		cli.StringFlag{
			Name:  "cpuprofile",
			Usage: "write cpu profile to `FILE`",
		},
		cli.StringFlag{
			Name:  "memprofile",
			Usage: "write memory profile to `FILE`",
		},
		cli.IntFlag{
			Name:  "blockprofile",
			Usage: "enable blocking profiler",
		},
		cli.StringFlag{
			Name:  "pprof",
			Usage: "run pprof server on `ADDR`",
		},
		cli.BoolFlag{
			Name:  "debug, d",
			Usage: "enable debug log",
		},
		cli.StringFlag{
			Name:  "logfile",
			Usage: "write log to `FILE`",
		},
	}
	app.Before = handleBeforeCommand
	app.After = handleAfterCommand
	app.Commands = []cli.Command{
		{
			Name:  "server",
			Usage: "run rpc server and torrent client",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "config",
					Usage: "path to the config file",
					Value: "~/.rain/config.yaml",
				},
			},
			Action: handleServer,
		},
		{
			Name:  "client",
			Usage: "send rpc request to server",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "url",
					Usage: "URL of RPC server",
					Value: "http://127.0.0.1:" + strconv.Itoa(session.DefaultConfig.RPCPort),
				},
			},
			Before: handleBeforeClient,
			Subcommands: []cli.Command{
				{
					Name:   "list",
					Usage:  "list torrents",
					Action: handleList,
				},
				{
					Name:   "add",
					Usage:  "add torrent or magnet",
					Action: handleAdd,
				},
				{
					Name:   "remove",
					Usage:  "remove torrent",
					Action: handleRemove,
				},
				{
					Name:   "stats",
					Usage:  "get stats of torrent",
					Action: handleStats,
				},
				{
					Name:   "trackers",
					Usage:  "get trackers of torrent",
					Action: handleTrackers,
				},
				{
					Name:   "peers",
					Usage:  "get peers of torrent",
					Action: handlePeers,
				},
				{
					Name:   "start",
					Usage:  "start torrent",
					Action: handleStart,
				},
				{
					Name:   "stop",
					Usage:  "stop",
					Action: handleStop,
				},
				{
					Name:   "console",
					Usage:  "show client console",
					Action: handleConsole,
				},
			},
		},
		{
			Name:   "boltbrowser",
			Hidden: true,
			Action: handleBoltBrowser,
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func handleBoltBrowser(c *cli.Context) error {
	db, err := bolt.Open(c.Args().Get(0), 0600, nil)
	if err != nil {
		return err
	}
	boltbrowser.Browse(db, false)
	return db.Close()
}

func handleBeforeCommand(c *cli.Context) error {
	cpuprofile := c.GlobalString("cpuprofile")
	if cpuprofile != "" {
		f, err := os.Create(cpuprofile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
	}
	pprofFlag := c.GlobalString("pprof")
	if pprofFlag != "" {
		go func() {
			log.Notice(http.ListenAndServe(pprofFlag, nil))
		}()
	}
	logFile := c.GlobalString("logfile")
	if logFile != "" {
		f, err := os.Create(logFile)
		if err != nil {
			log.Fatal("could not create log file: ", err)
		}
		logger.SetHandler(clog.NewFileHandler(f))
	}
	blockProfile := c.GlobalInt("blockprofile")
	if blockProfile != 0 {
		runtime.SetBlockProfileRate(blockProfile)
	}
	if c.GlobalBool("debug") {
		logger.SetLevel(clog.DEBUG)
	}
	return nil
}

func handleAfterCommand(c *cli.Context) error {
	if c.GlobalString("cpuprofile") != "" {
		pprof.StopCPUProfile()
	}
	return nil
}

func handleServer(c *cli.Context) error {
	cfg := session.DefaultConfig

	configPath := c.String("config")
	if configPath != "" {
		cp, err := homedir.Expand(configPath)
		if err != nil {
			return err
		}
		b, err := ioutil.ReadFile(cp) // nolint: gosec
		if os.IsNotExist(err) {
			log.Noticef("config file not found at %q, using default config", cp)
		} else if err != nil {
			return err
		} else {
			err = yaml.Unmarshal(b, &cfg)
			if err != nil {
				return err
			}
			log.Infoln("config loaded from:", cp)
			b, err = yaml.Marshal(&cfg)
			if err != nil {
				return err
			}
			log.Debug("\n" + string(b))
		}
	}

	ses, err := session.New(cfg)
	if err != nil {
		return err
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	s := <-ch
	log.Noticef("received %s, stopping server", s)

	memprofile := c.GlobalString("memprofile")
	if memprofile != "" {
		f, err := os.Create(memprofile)
		if err != nil {
			log.Fatal(err)
		}
		err = pprof.WriteHeapProfile(f)
		if err != nil {
			log.Fatal(err)
		}
		err = f.Close()
		if err != nil {
			log.Fatal(err)
		}
	}

	return ses.Close()
}

func handleBeforeClient(c *cli.Context) error {
	clt = rainrpc.NewClient(c.String("url"))
	return nil
}

func handleList(c *cli.Context) error {
	resp, err := clt.ListTorrents()
	if err != nil {
		return err
	}
	b, err := prettyjson.Marshal(resp)
	if err != nil {
		return err
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

func handleAdd(c *cli.Context) error {
	var b []byte
	var marshalErr error
	arg := c.Args().Get(0)
	if strings.HasPrefix(arg, "magnet:") {
		resp, err := clt.AddMagnet(arg)
		if err != nil {
			return err
		}
		b, marshalErr = prettyjson.Marshal(resp)
	} else {
		f, err := os.Open(arg) // nolint: gosec
		if err != nil {
			return err
		}
		resp, err := clt.AddTorrent(f)
		_ = f.Close()
		if err != nil {
			return err
		}
		b, marshalErr = prettyjson.Marshal(resp)
	}
	if marshalErr != nil {
		return marshalErr
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

func handleRemove(c *cli.Context) error {
	id, err := strconv.ParseUint(c.Args().Get(0), 10, 64)
	if err != nil {
		return err
	}
	_, err = clt.RemoveTorrent(id)
	return err
}

func handleStats(c *cli.Context) error {
	id, err := strconv.ParseUint(c.Args().Get(0), 10, 64)
	if err != nil {
		return err
	}
	resp, err := clt.GetTorrentStats(id)
	if err != nil {
		return err
	}
	b, err := prettyjson.Marshal(resp)
	if err != nil {
		return err
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

func handleTrackers(c *cli.Context) error {
	id, err := strconv.ParseUint(c.Args().Get(0), 10, 64)
	if err != nil {
		return err
	}
	resp, err := clt.GetTorrentTrackers(id)
	if err != nil {
		return err
	}
	b, err := prettyjson.Marshal(resp)
	if err != nil {
		return err
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

func handlePeers(c *cli.Context) error {
	id, err := strconv.ParseUint(c.Args().Get(0), 10, 64)
	if err != nil {
		return err
	}
	resp, err := clt.GetTorrentPeers(id)
	if err != nil {
		return err
	}
	b, err := prettyjson.Marshal(resp)
	if err != nil {
		return err
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

func handleStart(c *cli.Context) error {
	id, err := strconv.ParseUint(c.Args().Get(0), 10, 64)
	if err != nil {
		return err
	}
	_, err = clt.StartTorrent(id)
	return err
}

func handleStop(c *cli.Context) error {
	id, err := strconv.ParseUint(c.Args().Get(0), 10, 64)
	if err != nil {
		return err
	}
	_, err = clt.StopTorrent(id)
	return err
}

func handleConsole(c *cli.Context) error {
	con := console.New(clt)
	return con.Run()
}

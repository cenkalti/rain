package main

import (
	"errors"
	"fmt"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/log"
	"github.com/cenkalti/rain/client"
	"github.com/cenkalti/rain/internal/clientversion"
	"github.com/cenkalti/rain/internal/console"
	"github.com/cenkalti/rain/internal/jsonutil"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/rainrpc"
	"github.com/cenkalti/rain/torrent"
	"github.com/cenkalti/rain/torrent/resumer/boltdbresumer"
	"github.com/cenkalti/rain/torrent/storage/filestorage"
	"github.com/hokaccha/go-prettyjson"
	"github.com/jroimartin/gocui"
	"github.com/urfave/cli"
)

var (
	app = cli.NewApp()
	clt *rainrpc.Client
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
			Name:      "download",
			Usage:     "download torrent or magnet",
			ArgsUsage: "[torrent path or magnet link]",
			Action:    handleDownload,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "dest",
					Usage: "save files under `DIR`",
					Value: ".",
				},
				cli.IntFlag{
					Name:  "port",
					Usage: "peer listen port",
				},
				cli.BoolFlag{
					Name:  "seed",
					Usage: "continue seeding after download finishes",
				},
			},
		},
		{
			Name:  "client",
			Usage: "send request to RPC server",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "url",
					Usage: "URL of RPC server",
					Value: "localhost:7246",
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
			Name:  "server",
			Usage: "run RPC server",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "addr",
					Usage: "listen addr",
					Value: "0.0.0.0:7246",
				},
			},
			Action: handleServer,
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
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
	// configPath := c.GlobalString("config")
	// if configPath != "" {
	// 	cp, err := homedir.Expand(configPath)
	// 	if err != nil {
	// 		log.Fatal(err)
	// 	}
	// 	err = cfg.LoadFile(cp)
	// 	if err != nil {
	// 		log.Fatal(err)
	// 	}
	// }
	logFile := c.GlobalString("logfile")
	if logFile != "" {
		f, err := os.Create(logFile)
		if err != nil {
			log.Fatal("could not create log file: ", err)
		}
		logger.SetHandler(log.NewFileHandler(f))
	}
	if c.GlobalBool("debug") {
		logger.SetLevel(log.DEBUG)
	}
	return nil
}

func handleAfterCommand(c *cli.Context) error {
	if c.GlobalString("cpuprofile") != "" {
		pprof.StopCPUProfile()
	}
	return nil
}

func handleDownload(c *cli.Context) error {
	path := c.Args().Get(0)
	if path == "" {
		return errors.New("first argument must be a torrent file or magnet link")
	}
	sto, err := filestorage.New(c.String("dest"))
	if err != nil {
		return err
	}
	var t *torrent.Torrent
	if strings.HasPrefix(path, "magnet:") {
		t, err = torrent.NewMagnet(path, c.Int("port"), sto)
	} else {
		f, err2 := os.Open(path) // nolint: gosec
		if err2 != nil {
			return err
		}
		t, err = torrent.New(f, c.Int("port"), sto)
		_ = f.Close()
	}
	if err != nil {
		return err
	}
	defer t.Close()

	res, err := boltdbresumer.Open(t.Name()+"."+t.InfoHash()+".resume", []byte(t.InfoHash()))
	if err != nil {
		return err
	}
	defer func() { _ = res.Close() }()

	err = t.SetResume(res)
	if err != nil {
		return err
	}

	g, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		return err
	}
	defer g.Close()

	g.SetManagerFunc(func(g *gocui.Gui) error {
		x, y := g.Size()
		if v, err2 := g.SetView("stats", 0, 0, x-1, y-1); err != nil {
			if err2 != gocui.ErrUnknownView {
				return err2
			}
			v.Wrap = true
		}
		return nil
	})

	_ = g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		t.Stop()
		return nil
	})

	go func() {
		err = g.MainLoop()
		if err == gocui.ErrQuit {
			err = nil
		}
		if err != nil {
			log.Fatal(err)
		}
	}()

	go func() {
		for range time.Tick(100 * time.Millisecond) {
			s := t.Stats()
			b, err2 := jsonutil.MarshalCompactPretty(s)
			if err2 != nil {
				log.Fatal(err2)
			}
			g.Update(func(g *gocui.Gui) error {
				v, err2 := g.View("stats")
				if err2 != nil {
					return err2
				}
				v.Clear()
				_, _ = fmt.Fprintln(v, string(b))
				return nil
			})
		}
	}()

	t.Start()
	completeC := t.NotifyComplete()
	errC := t.NotifyError()
	for {
		select {
		case <-completeC:
			completeC = nil
			if !c.Bool("seed") {
				t.Stop()
				continue
			}
		case err = <-errC:
			if err != nil {
				err = res.Close()
			}
			return err
		}
	}
}

func handleServer(c *cli.Context) error {
	clt, _ := client.New(nil)
	addr := c.String("addr")
	srv := rainrpc.NewServer(clt)
	log.Infoln("RPC server is listening on", addr)
	return srv.ListenAndServe(addr)
}

func handleBeforeClient(c *cli.Context) error {
	var err error
	clt, err = rainrpc.NewClient(c.String("url"))
	return err
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

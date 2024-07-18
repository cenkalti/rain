package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"

	// nolint: gosec
	"io"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/boltbrowser/boltbrowser"
	"github.com/cenkalti/rain/internal/console"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/magnet"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/rainrpc"
	"github.com/cenkalti/rain/torrent"
	"github.com/hokaccha/go-prettyjson"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli"
	"github.com/zeebo/bencode"
	"gopkg.in/yaml.v2"
)

var (
	app = cli.NewApp()
	clt *rainrpc.Client
	log = logger.New("rain")
)

func main() {
	app.Version = torrent.Version
	app.Usage = "BitTorrent client from https://put.io"
	app.EnableBashCompletion = true
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "debug,d",
			Usage: "enable debug log",
		},
		cli.StringFlag{
			Name:   "cpuprofile",
			Hidden: true,
			Usage:  "write cpu profile to `FILE`",
		},
		cli.StringFlag{
			Name:   "memprofile",
			Hidden: true,
			Usage:  "write memory profile to `FILE`",
		},
		cli.IntFlag{
			Name:   "blockprofile",
			Hidden: true,
			Usage:  "enable blocking profiler",
		},
		cli.StringFlag{
			Name:   "pprof",
			Hidden: true,
			Usage:  "run pprof server on `ADDR`",
		},
	}
	app.Before = handleBeforeCommand
	app.After = handleAfterCommand
	app.Commands = []cli.Command{
		{
			Name:   "bash-autocomplete",
			Hidden: true,
			Usage:  "print bash autocompletion script",
			Action: printBashAutoComplete,
		},
		{
			Name:  "download",
			Usage: "download single torrent",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "config,c",
					Usage: "read config from `FILE`",
					Value: "~/rain/config.yaml",
				},
				cli.StringFlag{
					Name:     "torrent,t",
					Usage:    "torrent file or URI",
					Required: true,
				},
				cli.BoolFlag{
					// TODO fix flag letter
					Name:  "seed,d",
					Usage: "continue seeding after download is finished",
				},
				cli.StringFlag{
					Name:  "resume,r",
					Usage: "path to .resume file",
				},
			},
			Action: handleDownload,
		},
		{
			Name:  "magnet-to-torrent",
			Usage: "download torrent from magnet link",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "config,c",
					Usage: "read config from `FILE`",
					Value: "~/rain/config.yaml",
				},
				cli.StringFlag{
					Name:     "magnet,m",
					Usage:    "magnet link",
					Required: true,
				},
				cli.StringFlag{
					Name:  "output,o",
					Usage: "output file",
				},
				cli.DurationFlag{
					Name:  "timeout,t",
					Usage: "command fails if torrent cannot be downloaded after duration",
					Value: time.Minute,
				},
			},
			Action: handleMagnetToTorrent,
		},
		{
			Name:  "server",
			Usage: "run rpc server and torrent client",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "config,c",
					Usage: "read config from `FILE`",
					Value: "~/rain/config.yaml",
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
				{
					Name:     "version",
					Usage:    "server version",
					Category: "Getters",
					Action:   handleVersion,
				},
				{
					Name:     "list",
					Usage:    "list torrents",
					Category: "Getters",
					Action:   handleList,
				},
				{
					Name:     "add",
					Usage:    "add torrent or magnet",
					Category: "Actions",
					Action:   handleAdd,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "torrent,t",
							Usage:    "file or URI",
							Required: true,
						},
						cli.BoolFlag{
							Name:  "stopped",
							Usage: "do not start torrent automatically",
						},
						cli.BoolFlag{
							Name:  "stop-after-download",
							Usage: "stop the torrent after download is finished",
						},
						cli.BoolFlag{
							Name:  "stop-after-metadata",
							Usage: "stop the torrent after metadata download is finished",
						},
						cli.StringFlag{
							Name:  "id",
							Usage: "if id is not given, a unique id is automatically generated",
						},
					},
				},
				{
					Name:     "remove",
					Usage:    "remove torrent",
					Category: "Actions",
					Action:   handleRemove,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
					},
				},
				{
					Name:     "clean-database",
					Usage:    "clean session database",
					Category: "Actions",
					Action:   handleCleanDatabase,
				},
				{
					Name:     "stats",
					Usage:    "get stats of torrent",
					Category: "Getters",
					Action:   handleStats,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
						cli.BoolFlag{
							Name:  "json",
							Usage: "print raw stats as JSON",
						},
					},
				},
				{
					Name:     "session-stats",
					Usage:    "get stats of session",
					Category: "Getters",
					Action:   handleSessionStats,
					Flags: []cli.Flag{
						cli.BoolFlag{
							Name:  "json",
							Usage: "print raw stats as JSON",
						},
					},
				},
				{
					Name:     "trackers",
					Usage:    "get trackers of torrent",
					Category: "Getters",
					Action:   handleTrackers,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
					},
				},
				{
					Name:     "webseeds",
					Usage:    "get webseed sources of torrent",
					Category: "Getters",
					Action:   handleWebseeds,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
					},
				},
				{
					Name:     "peers",
					Usage:    "get peers of torrent",
					Category: "Getters",
					Action:   handlePeers,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
					},
				},
				{
					Name:     "add-peer",
					Usage:    "add peer to torrent",
					Category: "Actions",
					Action:   handleAddPeer,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
						cli.StringFlag{
							Name:     "addr",
							Usage:    "peer address in host:port format",
							Required: true,
						},
					},
				},
				{
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
				},
				{
					Name:     "announce",
					Usage:    "announce to tracker",
					Category: "Actions",
					Action:   handleAnnounce,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
					},
				},
				{
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
				},
				{
					Name:     "start",
					Usage:    "start torrent",
					Category: "Actions",
					Action:   handleStart,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
					},
				},
				{
					Name:     "stop",
					Usage:    "stop torrent",
					Category: "Actions",
					Action:   handleStop,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
					},
				},
				{
					Name:     "start-all",
					Usage:    "start all torrents",
					Category: "Actions",
					Action:   handleStartAll,
				},
				{
					Name:     "stop-all",
					Usage:    "stop all torrents",
					Category: "Actions",
					Action:   handleStopAll,
				},
				{
					Name:     "move",
					Usage:    "move torrent to another server",
					Category: "Actions",
					Action:   handleMove,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
						cli.StringFlag{
							Name:     "target",
							Required: true,
							Usage:    "target server in host:port format",
						},
					},
				},
				{
					Name:     "torrent",
					Usage:    "save torrent file",
					Category: "Getters",
					Action:   handleSaveTorrent,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "id",
							Required: true,
						},
						cli.StringFlag{
							Name:     "out,o",
							Required: true,
						},
					},
				},
				{
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
				},
				{
					Name:     "console",
					Usage:    "show client console",
					Category: "Other",
					Action:   handleConsole,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "columns",
							Value:    "# ID Name",
							Required: false,
						},
					},
				},
			},
		},
		{
			Name:   "boltbrowser",
			Hidden: true,
			Action: handleBoltBrowser,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:     "file,f",
					Required: true,
				},
			},
		},
		{
			Name:   "compact-database",
			Usage:  "rewrite database to save up space",
			Action: handleCompactDatabase,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "config,c",
					Usage: "read config from `FILE`",
					Value: "~/rain/config.yaml",
				},
			},
		},
		{
			Name:  "torrent",
			Usage: "manage torrent files",
			Subcommands: []cli.Command{
				{
					Name:   "show",
					Usage:  "show contents of the torrent file",
					Action: handleTorrentShow,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "file,f",
							Required: true,
						},
					},
				},
				{
					Name:   "infohash",
					Usage:  "calculate and print info-hash in torrent file",
					Action: handleInfoHash,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:     "file,f",
							Required: true,
						},
					},
				},
				{
					Name:   "create",
					Usage:  "create new torrent file",
					Action: handleTorrentCreate,
					Flags: []cli.Flag{
						cli.StringSliceFlag{
							Name:     "file,f",
							Usage:    "include this file or directory in torrent",
							Required: true,
						},
						cli.StringFlag{
							Name:     "out,o",
							Usage:    "save generated torrent to this `FILE`",
							Required: true,
						},
						cli.StringFlag{
							Name:  "root,r",
							Usage: "file paths given become relative to the root",
						},
						cli.StringFlag{
							Name:  "name,n",
							Usage: "set name of torrent. required if you specify more than one file.",
						},
						cli.BoolFlag{
							Name:  "private,p",
							Usage: "create torrent for private trackers",
						},
						cli.IntFlag{
							Name:  "piece-length,l",
							Usage: "override default piece length. by default, piece length calculated automatically based on the total size of files. given in KB. must be multiple of 16.",
						},
						cli.StringFlag{
							Name:  "comment,c",
							Usage: "add `COMMENT` to torrent",
						},
						cli.StringSliceFlag{
							Name:  "tracker,t",
							Usage: "add tracker `URL`",
						},
						cli.StringSliceFlag{
							Name:  "webseed,w",
							Usage: "add webseed `URL`",
						},
					},
				},
			},
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func handleBoltBrowser(c *cli.Context) error {
	db, err := bolt.Open(c.String("file"), 0600, nil)
	if err != nil {
		return err
	}
	boltbrowser.Browse(db, false)
	return db.Close()
}

func handleCompactDatabase(c *cli.Context) error {
	cfg, err := prepareConfig(c)
	if err != nil {
		return err
	}
	cfg.ResumeOnStartup = false
	cfg.RPCEnabled = false
	cfg.DHTEnabled = false
	ses, err := torrent.NewSession(cfg)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "rain-compact-database-")
	if err != nil {
		return err
	}
	err = f.Close()
	if err != nil {
		return err
	}
	err = ses.CompactDatabase(f.Name())
	if err != nil {
		return err
	}
	dbPath, err := homedir.Expand(cfg.Database)
	if err != nil {
		return err
	}
	err = os.Rename(dbPath, dbPath+".bak")
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), dbPath)
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
	blockProfile := c.GlobalInt("blockprofile")
	if blockProfile != 0 {
		runtime.SetBlockProfileRate(blockProfile)
	}
	if c.GlobalBool("debug") {
		logger.SetDebug()
	}
	return nil
}

func handleAfterCommand(c *cli.Context) error {
	if c.GlobalString("cpuprofile") != "" {
		pprof.StopCPUProfile()
	}
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
	return nil
}

func prepareConfig(c *cli.Context) (torrent.Config, error) {
	cfg := torrent.DefaultConfig

	configPath := c.String("config")
	if configPath != "" {
		cp, err := homedir.Expand(configPath)
		if err != nil {
			return cfg, err
		}
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

func handleServer(c *cli.Context) error {
	cfg, err := prepareConfig(c)
	if err != nil {
		return err
	}
	ses, err := torrent.NewSession(cfg)
	if err != nil {
		return err
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	s := <-ch
	log.Noticef("received %s, stopping server", s)

	return ses.Close()
}

func handleDownload(c *cli.Context) error {
	arg := c.String("torrent")
	seed := c.Bool("seed")
	resume := c.String("resume")
	cfg, err := prepareConfig(c)
	if err != nil {
		return err
	}
	cfg.DataDir = "."
	cfg.DataDirIncludesTorrentID = false
	var ih torrent.InfoHash
	if strings.HasPrefix(arg, "magnet:") {
		magnet, err := magnet.New(arg)
		if err != nil {
			return err
		}
		ih = torrent.InfoHash(magnet.InfoHash)
		cfg.Database = magnet.Name + ".resume"
	} else {
		var rc io.ReadCloser

		if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			resp, err := http.DefaultClient.Get(arg) // nolint: noctx
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			rc = resp.Body
		} else {
			f, err := os.Open(arg)
			if err != nil {
				return err
			}
			defer f.Close()
			rc = f
		}
		mi, err := metainfo.New(rc)
		if err != nil {
			return err
		}
		rc.Close()
		ih = mi.Info.Hash
		cfg.Database = mi.Info.Name + ".resume"
	}
	if resume != "" {
		cfg.Database = resume
	}
	ses, err := torrent.NewSession(cfg)
	if err != nil {
		return err
	}
	defer ses.Close()
	var t *torrent.Torrent
	torrents := ses.ListTorrents()
	if len(torrents) > 0 && torrents[0].InfoHash() == ih {
		// Resume data exists
		t = torrents[0]
		err = t.Start()
	} else {
		// Add as new torrent
		opt := &torrent.AddTorrentOptions{
			StopAfterDownload: !seed,
		}
		if isURI(arg) {
			t, err = ses.AddURI(arg, opt)
		} else {
			var f *os.File
			f, err = os.Open(arg)
			if err != nil {
				return err
			}
			t, err = ses.AddTorrent(f, opt)
			f.Close()
		}
	}
	if err != nil {
		return err
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	for {
		select {
		case s := <-ch:
			log.Noticef("received %s, stopping torrent", s)
			err = t.Stop()
			if err != nil {
				return err
			}
		case <-time.After(time.Second):
			stats := t.Stats()
			progress := 0
			if stats.Bytes.Total > 0 {
				progress = int((stats.Bytes.Completed * 100) / stats.Bytes.Total)
			}
			eta := "?"
			if stats.ETA != nil {
				eta = stats.ETA.String()
			}
			log.Infof("Status: %s, Progress: %d%%, Peers: %d, Speed: %dK/s, ETA: %s\n", stats.Status.String(), progress, stats.Peers.Total, stats.Speed.Download/1024, eta)
		case err = <-t.NotifyStop():
			return err
		}
	}
}

func handleMagnetToTorrent(c *cli.Context) error {
	arg := c.String("magnet")
	output := c.String("output")
	timeout := c.Duration("timeout")
	cfg, err := prepareConfig(c)
	if err != nil {
		return err
	}
	dbFile, err := os.CreateTemp("", "")
	if err != nil {
		return err
	}
	dbFileName := dbFile.Name()
	defer os.Remove(dbFileName)
	err = dbFile.Close()
	if err != nil {
		return err
	}
	cfg.Database = dbFileName
	ses, err := torrent.NewSession(cfg)
	if err != nil {
		return err
	}
	defer ses.Close()
	opt := &torrent.AddTorrentOptions{
		StopAfterMetadata: true,
	}
	t, err := ses.AddURI(arg, opt)
	if err != nil {
		return err
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	timeoutC := time.After(timeout)
	metadataC := t.NotifyMetadata()
	for {
		select {
		case s := <-ch:
			log.Noticef("received %s, stopping torrent", s)
			err = t.Stop()
			if err != nil {
				return err
			}
		case <-time.After(timeout):
			stats := t.Stats()
			log.Infof("Status: %s, Peers: %d\n", stats.Status.String(), stats.Peers.Total)
		case <-metadataC:
			name := output
			if name == "" {
				name = t.Name() + ".torrent"
			}
			data, err := t.Torrent()
			if err != nil {
				return err
			}
			f, err := os.Create(name)
			if err != nil {
				return err
			}
			_, err = f.Write(data)
			if err != nil {
				return err
			}
			err = f.Close()
			if err != nil {
				return err
			}
			fmt.Println(name)
			return nil
		case <-timeoutC:
			return fmt.Errorf("metadata cannot be downloaded in %s, try increasing timeout", timeout.String())
		case err = <-t.NotifyStop():
			return err
		}
	}
}

func handleBeforeClient(c *cli.Context) error {
	clt = rainrpc.NewClient(c.String("url"))
	clt.SetTimeout(c.Duration("timeout"))
	return nil
}

func handleVersion(c *cli.Context) error {
	version, err := clt.ServerVersion()
	if err != nil {
		return err
	}
	_, _ = os.Stdout.WriteString(version)
	_, _ = os.Stdout.WriteString("\n")
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

func isURI(arg string) bool {
	return strings.HasPrefix(arg, "magnet:") || strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://")
}

func handleAdd(c *cli.Context) error {
	var b []byte
	var marshalErr error
	arg := c.String("torrent")
	addOpt := &rainrpc.AddTorrentOptions{
		Stopped:           c.Bool("stopped"),
		StopAfterDownload: c.Bool("stop-after-download"),
		StopAfterMetadata: c.Bool("stop-after-metadata"),
		ID:                c.String("id"),
	}
	if isURI(arg) {
		resp, err := clt.AddURI(arg, addOpt)
		if err != nil {
			return err
		}
		b, marshalErr = prettyjson.Marshal(resp)
	} else {
		f, err := os.Open(arg)
		if err != nil {
			return err
		}
		resp, err := clt.AddTorrent(f, addOpt)
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
	return clt.RemoveTorrent(c.String("id"))
}

func handleCleanDatabase(c *cli.Context) error {
	return clt.CleanDatabase()
}

func handleStats(c *cli.Context) error {
	s, err := clt.GetTorrentStats(c.String("id"))
	if err != nil {
		return err
	}
	if c.Bool("json") {
		b, err := prettyjson.Marshal(s)
		if err != nil {
			return err
		}
		_, _ = os.Stdout.Write(b)
		_, _ = os.Stdout.WriteString("\n")
		return nil
	}
	console.FormatStats(s, os.Stdout)
	return nil
}

func handleSessionStats(c *cli.Context) error {
	s, err := clt.GetSessionStats()
	if err != nil {
		return err
	}
	if c.Bool("json") {
		b, err := prettyjson.Marshal(s)
		if err != nil {
			return err
		}
		_, _ = os.Stdout.Write(b)
		_, _ = os.Stdout.WriteString("\n")
		return nil
	}
	console.FormatSessionStats(s, os.Stdout)
	return nil
}

func handleTrackers(c *cli.Context) error {
	resp, err := clt.GetTorrentTrackers(c.String("id"))
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

func handleWebseeds(c *cli.Context) error {
	resp, err := clt.GetTorrentWebseeds(c.String("id"))
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
	resp, err := clt.GetTorrentPeers(c.String("id"))
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

func handleAddPeer(c *cli.Context) error {
	return clt.AddPeer(c.String("id"), c.String("addr"))
}

func handleAddTracker(c *cli.Context) error {
	return clt.AddTracker(c.String("id"), c.String("tracker"))
}

func handleAnnounce(c *cli.Context) error {
	return clt.AnnounceTorrent(c.String("id"))
}

func handleVerify(c *cli.Context) error {
	return clt.VerifyTorrent(c.String("id"))
}

func handleStart(c *cli.Context) error {
	return clt.StartTorrent(c.String("id"))
}

func handleStop(c *cli.Context) error {
	return clt.StopTorrent(c.String("id"))
}

func handleStartAll(c *cli.Context) error {
	return clt.StartAllTorrents()
}

func handleStopAll(c *cli.Context) error {
	return clt.StopAllTorrents()
}

func handleMove(c *cli.Context) error {
	return clt.MoveTorrent(c.String("id"), c.String("target"))
}

func handleConsole(c *cli.Context) error {
	columns := strings.Split(c.String("columns"), " ")

	con := console.New(clt, columns)
	return con.Run()
}

func handleTorrentShow(c *cli.Context) error {
	f, err := os.Open(c.String("file"))
	if err != nil {
		return err
	}
	defer f.Close()

	val := make(map[string]any)
	err = bencode.NewDecoder(f).Decode(&val)
	if err != nil {
		return err
	}
	if info, ok := val["info"].(map[string]any); ok {
		if pieces, ok := info["pieces"].(string); ok {
			info["pieces"] = fmt.Sprintf("<<< %d bytes of data >>>", len(pieces))
		}
	}
	b, err := prettyjson.Marshal(val)
	if err != nil {
		return err
	}
	_, _ = os.Stdout.Write(b)
	_, _ = os.Stdout.WriteString("\n")
	return nil
}

func handleInfoHash(c *cli.Context) error {
	f, err := os.Open(c.String("file"))
	if err != nil {
		return err
	}
	defer f.Close()

	var metainfo struct {
		Info bencode.RawMessage `bencode:"info"`
	}
	err = bencode.NewDecoder(f).Decode(&metainfo)
	if err != nil {
		return err
	}
	sum := sha1.Sum(metainfo.Info)
	fmt.Println(hex.EncodeToString(sum[:]))
	return nil
}

func handleTorrentCreate(c *cli.Context) error {
	paths := c.StringSlice("file")
	out := c.String("out")
	root := c.String("root")
	name := c.String("name")
	private := c.Bool("private")
	pieceLength := c.Uint("piece-length")
	comment := c.String("comment")
	trackers := c.StringSlice("tracker")
	webseeds := c.StringSlice("webseed")

	var err error
	out, err = homedir.Expand(out)
	if err != nil {
		return err
	}
	for i, path := range paths {
		paths[i], err = homedir.Expand(path)
		if err != nil {
			return err
		}
	}

	tiers := make([][]string, len(trackers))
	for i, tr := range trackers {
		tiers[i] = []string{tr}
	}

	info, err := metainfo.NewInfoBytes(root, paths, private, uint32(pieceLength<<10), name, log)
	if err != nil {
		return err
	}
	mi, err := metainfo.NewBytes(info, tiers, webseeds, comment)
	if err != nil {
		return err
	}
	log.Infof("Created torrent size: %d bytes", len(mi))
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	_, err = f.Write(mi)
	if err != nil {
		return err
	}
	return f.Close()
}

func handleSaveTorrent(c *cli.Context) error {
	torrent, err := clt.GetTorrent(c.String("id"))
	if err != nil {
		return err
	}
	f, err := os.Create(c.String("out"))
	if err != nil {
		return err
	}
	_, err = f.Write(torrent)
	if err != nil {
		return err
	}
	return f.Close()
}

func handleGetMagnet(c *cli.Context) error {
	magnet, err := clt.GetMagnet(c.String("id"))
	if err != nil {
		return err
	}
	fmt.Println(magnet)
	return nil
}

func printBashAutoComplete(c *cli.Context) error {
	fmt.Println(`#! /bin/bash
PROG=rain
_cli_bash_autocomplete() {
  if [[ "${COMP_WORDS[0]}" != "source" ]]; then
    local cur opts base
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    if [[ "$cur" == "-"* ]]; then
      opts=$( ${COMP_WORDS[@]:0:$COMP_CWORD} ${cur} --generate-bash-completion )
    else
      opts=$( ${COMP_WORDS[@]:0:$COMP_CWORD} --generate-bash-completion )
    fi
    COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
    return 0
  fi
}

complete -o bashdefault -o default -o nospace -F _cli_bash_autocomplete $PROG
unset PROG`)
	return nil
}

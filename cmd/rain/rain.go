package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"time"

	"github.com/cenkalti/log"
	"github.com/mitchellh/go-homedir"
	"gopkg.in/yaml.v2"

	"github.com/cenkalti/rain"
)

const defaultConfig = "~/.rain.yml"

var (
	config             = flag.String("c", defaultConfig, "config file")
	where              = flag.String("w", "", "where to download")
	port               = flag.Int("p", int(rain.DefaultConfig.Port), "listen port for incoming peer connections")
	debug              = flag.Bool("d", false, "enable debug log")
	version            = flag.Bool("v", false, "version")
	exitAfterCompleted = flag.Bool("e", false, "exit after files are downloaded")
)

func main() {
	flag.Parse()

	if *version == true {
		fmt.Println(rain.Version)
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Give a torrent file as first argument!")
		os.Exit(1)
	}

	rand.Seed(time.Now().UnixNano())

	if *debug {
		rain.SetLogLevel(log.DEBUG)
	}

	var configFile string
	if *config != "" {
		configFile = *config
	} else {
		configFile = defaultConfig
	}

	var err error
	configFile, err = homedir.Expand(configFile)
	if err != nil {
		fmt.Fprint(os.Stderr, "Cannot determine home directory! Specify config file with -c flag.")
		os.Exit(1)
	}

	c, err := LoadConfig(configFile)
	if err != nil {
		log.Fatal(err)
	}

	c.Port = *port
	if *where != "" {
		c.DownloadDir = *where
	}

	r, err := rain.NewClient(c)
	if err != nil {
		log.Fatal(err)
	}

	err = r.Listen()
	if err != nil {
		log.Fatal(err)
	}

	t, err := r.Add(args[0])
	if err != nil {
		log.Fatal(err)
	}

	t.Start()

	if *exitAfterCompleted {
		<-t.CompleteNotify()
	} else {
		select {}
	}
}

func LoadConfig(filename string) (*rain.Config, error) {
	c := rain.DefaultConfig
	b, err := ioutil.ReadFile(filename)
	if os.IsNotExist(err) {
		return &c, nil
	}
	if err != nil {
		return nil, err
	}
	if err = yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

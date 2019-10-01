module github.com/cenkalti/rain

go 1.13

require (
	github.com/boltdb/bolt v1.3.1
	github.com/cenkalti/backoff/v3 v3.0.0
	github.com/cenkalti/boltbrowser v0.0.0-20190327195521-ebed13c76690
	github.com/cenkalti/log v0.0.0-20180808170110-e1cf6d40cbc3
	github.com/chihaya/chihaya v0.0.0-20190113170711-5f99a7e77885
	github.com/cyberdelia/go-metrics-graphite v0.0.0-20161219230853-39f87cc3b432
	github.com/fatih/color v1.7.0 // indirect
	github.com/fatih/structs v1.1.0
	github.com/gofrs/uuid v3.2.0+incompatible
	github.com/google/btree v1.0.0
	github.com/hashicorp/go-multierror v1.0.0 // indirect
	github.com/hokaccha/go-prettyjson v0.0.0-20190818114111-108c894c2c0e
	github.com/jroimartin/gocui v0.4.0
	github.com/juju/ratelimit v1.0.1
	github.com/mattn/go-colorable v0.1.4 // indirect
	github.com/mattn/go-isatty v0.0.9 // indirect
	github.com/mitchellh/go-homedir v1.1.0
	github.com/multiformats/go-multihash v0.0.8
	github.com/nictuku/dht v0.0.0-20190424204932-20d30c21bd4c
	github.com/powerman/rpc-codec v1.1.3
	github.com/rcrowley/go-metrics v0.0.0-20190826022208-cac0b30c2563
	github.com/urfave/cli v1.22.1
	github.com/zeebo/bencode v1.0.0
	golang.org/x/sys v0.0.0-20191001151750-bb3f8db39f24
	gopkg.in/yaml.v2 v2.2.3
)

replace github.com/rcrowley/go-metrics => github.com/cenkalti/go-metrics v0.0.0-20190826022208-cac0b30c2563

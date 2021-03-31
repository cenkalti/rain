module github.com/cenkalti/rain

go 1.16

require (
	github.com/boltdb/bolt v1.3.1
	github.com/br0xen/termbox-util v0.0.0-20200220160819-dc6d6950ba00 // indirect
	github.com/cenkalti/backoff/v3 v3.2.2
	github.com/cenkalti/boltbrowser v0.0.0-20190327195521-ebed13c76690
	github.com/cenkalti/log v0.0.0-20180808170110-e1cf6d40cbc3
	github.com/chihaya/chihaya v1.0.1-0.20191017040149-0a420fe05344
	github.com/cpuguy83/go-md2man/v2 v2.0.0 // indirect
	github.com/fatih/color v1.9.0 // indirect
	github.com/fatih/structs v1.1.0
	github.com/fortytw2/leaktest v1.3.0
	github.com/gofrs/uuid v3.2.0+incompatible
	github.com/golang/groupcache v0.0.0-20200121045136-8c9f03a8e57e // indirect
	github.com/google/btree v1.0.0
	github.com/hashicorp/go-multierror v1.1.0 // indirect
	github.com/hokaccha/go-prettyjson v0.0.0-20190818114111-108c894c2c0e
	github.com/jroimartin/gocui v0.4.0
	github.com/juju/ratelimit v1.0.1
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/mattn/go-colorable v0.1.6 // indirect
	github.com/mattn/go-runewidth v0.0.9 // indirect
	github.com/minio/sha256-simd v0.1.1 // indirect
	github.com/mitchellh/go-homedir v1.1.0
	github.com/multiformats/go-multihash v0.0.13
	github.com/nictuku/dht v0.0.0-20200512202754-8008414d60e5
	github.com/nsf/termbox-go v0.0.0-20200418040025-38ba6e5628f1 // indirect
	github.com/powerman/rpc-codec v1.1.4
	github.com/prometheus/client_golang v1.1.0 // indirect
	github.com/prometheus/client_model v0.0.0-20190812154241-14fe0d1b01d4 // indirect
	github.com/prometheus/common v0.7.0 // indirect
	github.com/prometheus/procfs v0.0.5 // indirect
	github.com/rcrowley/go-metrics v0.0.0-20200313005456-10cdbea86bc0
	github.com/stretchr/testify v1.4.0
	github.com/urfave/cli v1.22.4
	github.com/youtube/vitess v2.2.0-rc.1+incompatible // indirect
	github.com/zeebo/bencode v1.0.0
	go.etcd.io/bbolt v1.3.5
	golang.org/x/crypto v0.0.0-20200414173820-0848c9571904 // indirect
	golang.org/x/sys v0.0.0-20200413165638-669c56c373c4
	gopkg.in/yaml.v2 v2.2.8
)

replace github.com/rcrowley/go-metrics => github.com/cenkalti/go-metrics v0.0.0-20190910102919-35c391953d1c

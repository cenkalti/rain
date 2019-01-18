package blocklist

import "net"

type Blocklist interface {
	Blocked(net.IP) bool
}

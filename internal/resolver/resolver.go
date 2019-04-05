package resolver

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/cenkalti/rain/internal/blocklist"
)

var (
	errBlocked          = errors.New("ip is blocked")
	errIPv6NotSupported = errors.New("ipv6 is not supported")
	errNotIPv4Address   = errors.New("not ipv4 address")
)

func Resolve(ctx context.Context, hostport string, timeout time.Duration, bl *blocklist.Blocklist) (net.IP, int, error) {
	var cancel func()
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, 0, err
	}
	ip := net.ParseIP(host)
	if ip != nil {
		// addrress is in <ip>:<port> format
		i4 := ip.To4()
		if i4 == nil {
			return nil, 0, errIPv6NotSupported
		}
		if bl != nil && bl.Blocked(ip) {
			return nil, 0, errBlocked
		}
		return i4, port, nil
	}
	var ips []net.IP
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, 0, err
	}
	for _, ia := range addrs {
		i4 := ia.IP.To4()
		if i4 != nil {
			ips = append(ips, i4)
		}
	}
	if len(ips) == 0 {
		return nil, 0, errNotIPv4Address
	}
	if bl != nil {
		for _, ip := range ips {
			if !bl.Blocked(ip) {
				return ip, port, nil
			}
		}
		return nil, 0, errors.New("ip is blocked")
	}
	return ips[0], port, nil
}

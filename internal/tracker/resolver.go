package tracker

import (
	"context"
	"errors"
	"net"
	"strconv"

	"github.com/cenkalti/rain/internal/blocklist"
)

func ResolveHost(ctx context.Context, addr string, bl *blocklist.Blocklist) (net.IP, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, 0, err
	}
	ip := net.ParseIP(host)
	if ip != nil {
		i4 := ip.To4()
		if i4 != nil {
			return i4, port, nil
		}
		return nil, 0, errors.New("ipv6 is not supported")
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
		return nil, 0, errors.New("not ipv4 address")
	}
	if bl != nil {
		for _, ip := range ips {
			if bl.Blocked(ip) {
				return nil, 0, errors.New("ip is blocked")
			}
		}
	}
	return ips[0], port, nil
}

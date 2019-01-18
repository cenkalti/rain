package tracker

import (
	"context"
	"errors"
	"net"
	"strconv"

	"github.com/cenkalti/rain/torrent/blocklist"
)

func ResolveHost(ctx context.Context, addr string, bl blocklist.Blocklist) (net.IP, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, 0, err
	}
	var ips []net.IP
	ip := net.ParseIP(host)
	if ip != nil {
		ips = append(ips, ip)
	} else {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, 0, err
		}
		for _, ia := range addrs {
			ips = append(ips, ia.IP)
		}
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

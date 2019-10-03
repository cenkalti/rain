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
	// ErrBlocked indicates that the resolved IP is blocked in the blocklist.
	ErrBlocked = errors.New("ip is blocked")
	// ErrNotIPv4Address indicates that the resolved IP address is not IPv4.
	ErrNotIPv4Address = errors.New("not ipv4 address")
	// ErrInvalidPort indicates that the port number in the address is invalid.
	ErrInvalidPort = errors.New("invalid port number")
)

// Resolve `hostport` to an IPv4 address.
func Resolve(ctx context.Context, hostport string, timeout time.Duration, bl *blocklist.Blocklist) (net.IP, int, error) {
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, 0, err
	}
	if port <= 0 || port > 65535 {
		return nil, 0, ErrInvalidPort
	}
	ip := net.ParseIP(host)
	if ip == nil {
		ip, err = ResolveIPv4(ctx, timeout, host)
		if err != nil {
			return nil, 0, err
		}
	}
	i4 := ip.To4()
	if i4 == nil {
		return nil, 0, ErrNotIPv4Address
	}
	if bl != nil && bl.Blocked(ip) {
		return nil, 0, ErrBlocked
	}
	return i4, port, nil
}

// ResolveIPv4 resolves `host` to and IPv4 address.
func ResolveIPv4(ctx context.Context, timeout time.Duration, host string) (net.IP, error) {
	var cancel func()
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ia := range addrs {
		i4 := ia.IP.To4()
		if i4 != nil {
			return i4, nil
		}
	}
	return nil, ErrNotIPv4Address
}

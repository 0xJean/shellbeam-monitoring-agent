package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"sync/atomic"
	"syscall"
	"time"
)

// errPingTargetRefused marks a ping stopped by the target policy. The task is
// reported as failed, exactly like an unreachable target.
var errPingTargetRefused = errors.New("ping target refused: private or local address")

// lookupPingHost is replaced in tests to simulate DNS answers.
var lookupPingHost = net.LookupHost

var pingRefusalLogged atomic.Bool

// refusedPingPrefixes lists what a compromised server must not be able to probe
// through this agent. Addresses are compared after unmapping IPv4-mapped IPv6.
var refusedPingPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT, and Alibaba Cloud metadata
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("168.63.129.16/32"), // Azure platform services, publicly numbered
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"), // includes Oracle Cloud metadata
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),

	netip.MustParsePrefix("::/96"),           // unspecified, loopback, IPv4-compatible
	netip.MustParsePrefix("::ffff:0:0:0/96"), // IPv4-translated
	netip.MustParsePrefix("64:ff9b:1::/48"),  // local-use NAT64
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"), // Teredo
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fc00::/7"), // unique local, and AWS IPv6 metadata
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

var (
	nat64Prefix = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour   = netip.MustParsePrefix("2002::/16")
)

func pingAddrRefused(addr netip.Addr) bool {
	addr = addr.WithZone("").Unmap()
	for _, prefix := range refusedPingPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	// These forms carry an IPv4 destination that a gateway would reach.
	raw := addr.As16()
	switch {
	case !addr.Is6():
		return false
	case nat64Prefix.Contains(addr):
		return pingAddrRefused(netip.AddrFrom4([4]byte(raw[12:16])))
	case sixToFour.Contains(addr):
		return pingAddrRefused(netip.AddrFrom4([4]byte(raw[2:6])))
	}
	return false
}

// checkPingAddress applies the target policy to the address about to be
// contacted. Anything that does not parse as an IP address is refused.
func checkPingAddress(host string) error {
	if flags.AllowPrivatePingTargets {
		return nil
	}
	addr, err := netip.ParseAddr(host)
	if err == nil && !pingAddrRefused(addr) {
		return nil
	}
	if pingRefusalLogged.CompareAndSwap(false, true) {
		log.Printf("[warn] refused a ping to %s, a private or local address; further refusals are not logged. Set --allow-private-ping-targets to monitor internal hosts", host)
	}
	return fmt.Errorf("%w: %s", errPingTargetRefused, host)
}

// dialPingTarget runs the check on the socket, after resolution and before the
// connection is made, so the address checked is the address contacted whatever
// a DNS answer said earlier. Each dial resolves the host once.
func dialPingTarget(ctx context.Context, network, host, port string, timeout time.Duration) (net.Conn, error) {
	ip, err := resolveIP(host)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: timeout, Control: pingDialControl}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
}

func pingDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", errPingTargetRefused, address)
	}
	return checkPingAddress(host)
}

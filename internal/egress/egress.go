// Package egress holds the policy for outbound HTTP the tracker makes on a user's
// behalf. It lives apart from both the worker that delivers webhooks and the service
// that accepts their URLs, because both need it and worker already imports service.
package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Outbound webhook delivery is a request the tracker makes to an address a user chose.
// That is the definition of SSRF, and the only thing standing in front of it was a
// check that the string began with "http": a `maintainer` could point a hook at
// 169.254.169.254 and read the cloud metadata service, or sweep the internal network
// using the persisted response_code as an oracle.
//
// The guard is applied at dial time rather than by parsing the URL, because parsing
// cannot survive DNS: a hostname that resolves to a public address when the webhook is
// created can resolve to 127.0.0.1 by the time it is delivered, and a redirect can
// move the target after the check. Checking the actual IP at the moment of connection
// is the only placement that closes both.

// ErrBlockedAddress is returned when a delivery target resolves to an address the
// tracker will not connect to.
var ErrBlockedAddress = errors.New("destination address is not permitted")

// blockedNets are the ranges no user-supplied URL may reach. Loopback and link-local
// are the dangerous ones (the metadata service lives at 169.254.169.254); the private
// ranges are here because a self-hosted tracker sits inside the network it would
// otherwise be used to scan.
var blockedNets = func() []*net.IPNet {
	cidrs := []string{
		"0.0.0.0/8",      // "this host"
		"10.0.0.0/8",     // RFC1918
		"127.0.0.0/8",    // loopback
		"169.254.0.0/16", // link-local, incl. cloud metadata
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"100.64.0.0/10",  // carrier-grade NAT / tailscale
		"192.0.0.0/24",   // IETF protocol assignments
		"198.18.0.0/15",  // benchmarking
		"::1/128",        // loopback
		"fc00::/7",       // unique local
		"fe80::/10",      // link-local
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// IsBlockedIP reports whether an address is off-limits for user-directed egress.
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// Normalise IPv4-mapped IPv6 (::ffff:127.0.0.1) down to plain IPv4 before testing,
	// so a mapped loopback is judged as loopback.
	//
	// This must be a conversion and not an extra ::ffff:0:0/96 entry in the list below:
	// net.IP holds IPv4 in the 16-byte mapped form, so that CIDR matches *every* IPv4
	// address and would have blocked all outbound webhooks. A test caught it.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateWebhookURL checks the parts of a destination that can be judged without
// resolving it. It is a courtesy check for the API — the binding guarantee is the
// dial-time one below — so it rejects the obvious cases early, where the person
// creating the webhook can still see the error.
func ValidateWebhookURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("not a valid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("must be an http(s) URL")
	}
	if u.Host == "" {
		return errors.New("must include a host")
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return ErrBlockedAddress
	}
	// A literal IP can be judged now. A hostname cannot — see the dialer.
	if ip := net.ParseIP(host); ip != nil && IsBlockedIP(ip) {
		return ErrBlockedAddress
	}
	return nil
}

// guardedDialer refuses to connect to a blocked address. Placed on the transport's
// DialContext so it sees the address actually being connected to, after DNS and after
// any redirect — the two places a URL-time check is defeated.
func GuardedDialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		// Every answer must be permitted, not merely the one we happen to pick: a
		// round-robin record with one public and one private address must not be a
		// coin flip.
		for _, ip := range ips {
			if IsBlockedIP(ip.IP) {
				return nil, fmt.Errorf("%w: %s resolves to %s", ErrBlockedAddress, host, ip.IP)
			}
		}
		// Dial the address we just vetted rather than the name, so no second lookup
		// can return something different (a DNS-rebinding race).
		return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
}

// webhookClient is shared across deliveries.
//
// It used to be constructed inline per delivery, which meant no connection reuse at
// all and no bound on how many sockets a burst could open. Redirects are refused
// outright: a 302 to 127.0.0.1 is the standard way around an egress check, and a
// webhook receiver has no legitimate reason to redirect.
var WebhookClient = sync.OnceValue(func() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext:         GuardedDialer(),
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 4,
			MaxConnsPerHost:     8,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 5 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
})

// hostGate bounds how many deliveries may be in flight to one host at a time.
//
// Every destination shares one River queue with MaxWorkers: 5, so five deliveries to
// an endpoint that black-holes connections consumed the entire webhook queue for the
// duration of their timeouts and every other integration stopped. The gate makes a
// slow endpoint slow only for itself.
type HostGate struct {
	mu    sync.Mutex
	slots map[string]chan struct{}
	limit int
}

func NewHostGate(limit int) *HostGate {
	return &HostGate{slots: map[string]chan struct{}{}, limit: limit}
}

// acquire blocks until this host has a free slot or ctx is done. The returned function
// releases it.
func (g *HostGate) Acquire(ctx context.Context, host string) (func(), error) {
	g.mu.Lock()
	ch, ok := g.slots[host]
	if !ok {
		ch = make(chan struct{}, g.limit)
		g.slots[host] = ch
	}
	g.mu.Unlock()

	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

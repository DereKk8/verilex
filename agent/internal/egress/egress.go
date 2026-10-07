// Package egress is the brain's only way to the network: an HTTP CONNECT proxy that reaches
// public addresses only. It refuses loopback, private and link-local networks and the host's own
// addresses, where the run's product instances and the host's services live, and it logs every
// request for the run's audit.
package egress

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Proxy serves CONNECT requests until Close.
type Proxy struct {
	ln    net.Listener
	log   io.Writer
	mu    sync.Mutex
	conns map[net.Conn]bool
	done  bool
}

// Listen opens the proxy on network and address ("unix" and a socket path, or "tcp" and a
// loopback address). log receives one line per request.
func Listen(network, address string, log io.Writer) (*Proxy, error) {
	ln, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}
	return &Proxy{ln: ln, log: log, conns: map[net.Conn]bool{}}, nil
}

// Addr is where the proxy listens.
func (p *Proxy) Addr() net.Addr { return p.ln.Addr() }

// Serve accepts until Close.
func (p *Proxy) Serve() {
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			return
		}
		if !p.track(conn) {
			conn.Close()
			return
		}
		go func() {
			defer p.untrack(conn)
			p.handle(conn)
		}()
	}
}

// Close stops the listener and every open tunnel.
func (p *Proxy) Close() error {
	err := p.ln.Close()
	p.mu.Lock()
	p.done = true
	conns := p.conns
	p.conns = map[net.Conn]bool{}
	p.mu.Unlock()
	for conn := range conns {
		conn.Close()
	}
	return err
}

func (p *Proxy) track(conn net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		return false
	}
	p.conns[conn] = true
	return true
}

func (p *Proxy) untrack(conn net.Conn) {
	p.mu.Lock()
	delete(p.conns, conn)
	p.mu.Unlock()
	conn.Close()
}

func (p *Proxy) logf(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.log, "%s %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

func (p *Proxy) handle(conn net.Conn) {
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}
	if req.Method != http.MethodConnect {
		p.logf("refused %s %s: only CONNECT is served", req.Method, req.URL)
		reply(conn, http.StatusMethodNotAllowed)
		return
	}
	host, port, err := net.SplitHostPort(req.Host)
	if err != nil {
		p.logf("refused CONNECT %q: %v", req.Host, err)
		reply(conn, http.StatusBadRequest)
		return
	}
	addr, err := pick(host)
	if err != nil {
		p.logf("refused CONNECT %s: %v", req.Host, err)
		reply(conn, http.StatusForbidden)
		return
	}
	upstream, err := net.DialTimeout("tcp", net.JoinHostPort(addr.String(), port), 30*time.Second)
	if err != nil {
		p.logf("failed CONNECT %s -> %s: %v", req.Host, addr, err)
		reply(conn, http.StatusBadGateway)
		return
	}
	if !p.track(upstream) {
		upstream.Close()
		return
	}
	defer p.untrack(upstream)
	p.logf("CONNECT %s -> %s", req.Host, addr)
	conn.SetReadDeadline(time.Time{})
	if _, err = io.WriteString(conn, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		return
	}
	go func() {
		if n := reader.Buffered(); n > 0 {
			early, _ := reader.Peek(n)
			upstream.Write(early)
		}
		io.Copy(upstream, conn)
		upstream.Close()
	}()
	io.Copy(conn, upstream)
}

func reply(conn net.Conn, code int) {
	fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", code, http.StatusText(code))
}

// pick resolves host once and returns the first address the brain may reach. The proxy dials that
// address itself, so a name cannot resolve to a public address here and a private one later.
func pick(host string) (netip.Addr, error) {
	var addrs []netip.Addr
	if addr, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		addrs = []netip.Addr{addr}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if addrs, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host); err != nil {
			return netip.Addr{}, err
		}
	}
	own := ownAddrs()
	for _, addr := range addrs {
		if Public(addr) && !own[addr.Unmap()] {
			return addr.Unmap(), nil
		}
	}
	return netip.Addr{}, fmt.Errorf("%s is not a public address the brain may reach", joinAddrs(addrs))
}

// reserved holds the ranges that are not public even though netip calls them global unicast.
var reserved = func() []netip.Prefix {
	var out []netip.Prefix
	for _, cidr := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/32", "2001:2::/48", "2001:db8::/32",
		"2002::/16", "fec0::/10",
	} {
		out = append(out, netip.MustParsePrefix(cidr))
	}
	return out
}()

// Public reports whether addr is a public unicast address: not loopback, private, link-local,
// carrier-grade NAT, documentation, translation or otherwise reserved.
func Public(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for _, prefix := range reserved {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// ownAddrs are the host's interface addresses: a product instance listening on all interfaces is
// reachable through them even when they are public.
func ownAddrs() map[netip.Addr]bool {
	out := map[netip.Addr]bool{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		if prefix, err := netip.ParsePrefix(a.String()); err == nil {
			out[prefix.Addr().Unmap()] = true
		}
	}
	return out
}

func joinAddrs(addrs []netip.Addr) string {
	if len(addrs) == 0 {
		return "no address"
	}
	parts := make([]string, len(addrs))
	for i, addr := range addrs {
		parts[i] = addr.String()
	}
	return strings.Join(parts, ", ")
}

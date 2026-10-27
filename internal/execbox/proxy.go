package execbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The proxy is the sandbox's only way out: an HTTP proxy (CONNECT for
// TLS, absolute URLs for plain HTTP) that admits only allowed hosts and
// never connects to this machine, private networks or link-local addresses
// (cloud metadata), whatever a name resolves to. It listens on a Unix
// socket that only the sandbox's bridge can reach.

type proxyHandle struct {
	dir    string
	socket string
	ln     net.Listener
	srv    *http.Server
}

func (p *proxyHandle) close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = p.srv.Shutdown(ctx)
	_ = p.srv.Close()
	_ = os.RemoveAll(p.dir)
}

func startProxy(stateDir string, hosts []string, onNet func(NetEvent), _ bool) (*proxyHandle, error) {
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	dir := filepath.Join(stateDir, "net-"+hex.EncodeToString(rnd[:]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	sock := filepath.Join(dir, "p.sock")
	if len(sock) > 100 { // sun_path limit
		_ = os.RemoveAll(dir)
		dir = filepath.Join(os.TempDir(), "bcnet-"+hex.EncodeToString(rnd[:]))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		sock = filepath.Join(dir, "p.sock")
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	p := &proxyHandle{dir: dir, socket: sock, ln: ln}
	h := &proxy{allow: hosts, onNet: onNet}
	p.srv = &http.Server{Handler: h, ReadHeaderTimeout: 30 * time.Second}
	go func() { _ = p.srv.Serve(ln) }()
	return p, nil
}

type proxy struct {
	allow []string
	onNet func(NetEvent)
	mu    sync.Mutex
}

// hostAllowed matches exact hosts, ".example.com" for subdomains, and "*".
func hostAllowed(allow []string, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, a := range allow {
		a = strings.ToLower(a)
		switch {
		case a == "*", a == host:
			return true
		case strings.HasPrefix(a, ".") && (strings.HasSuffix(host, a) || host == a[1:]):
			return true
		case strings.HasPrefix(a, "*.") && strings.HasSuffix(host, a[1:]):
			return true
		}
	}
	return false
}

func (p *proxy) report(e NetEvent) {
	if p.onNet != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.onNet(e)
	}
}

func (p *proxy) deny(w http.ResponseWriter, host, port, why string) {
	p.report(NetEvent{Host: host, Port: port, Reason: why})
	http.Error(w, "BoundedCode sandbox: network access to "+host+" is not allowed ("+why+"). "+
		"The user can allow it, or run the command when they approve it.", http.StatusForbidden)
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var host, port string
	if r.Method == http.MethodConnect {
		host, port, _ = net.SplitHostPort(r.Host)
	} else if r.URL.IsAbs() {
		host, port = r.URL.Hostname(), r.URL.Port()
		if port == "" {
			port = map[string]string{"https": "443"}[r.URL.Scheme]
			if port == "" {
				port = "80"
			}
		}
	}
	if host == "" {
		http.Error(w, "BoundedCode sandbox proxy: not a proxy request", http.StatusBadRequest)
		return
	}
	if !hostAllowed(p.allow, host) {
		p.deny(w, host, port, "not an allowed host")
		return
	}
	conn, err := dialPublic(r.Context(), host, port)
	if err != nil {
		p.deny(w, host, port, err.Error())
		return
	}
	p.report(NetEvent{Host: host, Port: port, Allowed: true})
	if r.Method == http.MethodConnect {
		hj, ok := w.(http.Hijacker)
		if !ok {
			conn.Close()
			http.Error(w, "hijack unsupported", http.StatusInternalServerError)
			return
		}
		client, buf, err := hj.Hijack()
		if err != nil {
			conn.Close()
			return
		}
		_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		if n := buf.Reader.Buffered(); n > 0 {
			b, _ := buf.Reader.Peek(n)
			_, _ = conn.Write(b)
		}
		splice(client, conn)
		return
	}
	// Plain HTTP: send the request on the connection already checked.
	defer conn.Close()
	out := r.Clone(r.Context())
	out.RequestURI = ""
	for _, h := range []string{"Proxy-Connection", "Proxy-Authorization", "Proxy-Authenticate"} {
		out.Header.Del(h)
	}
	tr := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return conn, nil }, DisableKeepAlives: true}
	resp, err := tr.RoundTrip(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func splice(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		_ = closeWrite(dst)
	}
	go cp(a, b)
	go cp(b, a)
	wg.Wait()
	a.Close()
	b.Close()
}

// dialPublic resolves host and connects to a public address only.
func dialPublic(ctx context.Context, host, port string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %s", host)
	}
	var lastErr error = errors.New("no public address")
	for _, ip := range ips {
		if !publicIP(ip.IP) {
			lastErr = fmt.Errorf("%s resolves to a local or private address", host)
			continue
		}
		var d net.Dialer
		c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return c, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func publicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() || isCGNAT(ip))
}

func isCGNAT(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64
}

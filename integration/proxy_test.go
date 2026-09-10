package integration_test

import (
	"context"
	"io"
	"net"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"
)

// databaseURL requires an explicitly supplied disposable PostgreSQL endpoint.
func databaseURL(t *testing.T) string {
	t.Helper()
	raw := os.Getenv("HARDCORE_TEST_DATABASE_URL")
	if raw == "" {
		t.Fatal("HARDCORE_TEST_DATABASE_URL is required; run make test-integration")
	}
	return raw
}

// tcpGate forwards real PostgreSQL traffic and can disconnect only its own
// clients. It never stops a shared database or terminates unrelated sessions.
type tcpGate struct {
	listener    net.Listener
	upstream    string
	mu          sync.Mutex
	blocked     bool
	stopped     bool
	connections map[net.Conn]struct{}
	wg          sync.WaitGroup
}

// newGate redirects a test URL through an ephemeral local TCP listener.
func newGate(t *testing.T) (*tcpGate, string) {
	t.Helper()
	u, err := url.Parse(databaseURL(t))
	if err != nil || u.Hostname() == "" || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("a PostgreSQL URL with a TCP host is required")
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gate := &tcpGate{listener: listener, upstream: net.JoinHostPort(u.Hostname(), port), connections: make(map[net.Conn]struct{})}
	t.Cleanup(gate.close)
	gate.wg.Add(1)
	go gate.accept()
	u.Host = listener.Addr().String()
	return gate, u.String()
}

// accept tracks workers before shutdown waits, including in-flight upstream dials.
func (g *tcpGate) accept() {
	defer g.wg.Done()
	for {
		client, err := g.listener.Accept()
		if err != nil {
			return
		}
		g.wg.Add(1)
		go g.forward(client)
	}
}

// forward copies both directions; closing either side tears down the pair.
func (g *tcpGate) forward(client net.Conn) {
	defer g.wg.Done()
	defer client.Close()
	g.mu.Lock()
	blocked := g.blocked || g.stopped
	g.mu.Unlock()
	if blocked {
		return
	}
	upstream, err := (&net.Dialer{Timeout: time.Second}).DialContext(context.Background(), "tcp", g.upstream)
	if err != nil {
		return
	}
	defer upstream.Close()
	g.mu.Lock()
	if g.blocked || g.stopped {
		g.mu.Unlock()
		return
	}
	g.connections[client] = struct{}{}
	g.connections[upstream] = struct{}{}
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.connections, client)
		delete(g.connections, upstream)
		g.mu.Unlock()
	}()
	done := make(chan struct{})
	go func() { _, _ = io.Copy(upstream, client); _ = upstream.Close(); close(done) }()
	_, _ = io.Copy(client, upstream)
	_ = client.Close()
	<-done
}

// setBlocked severs established sessions as well as rejecting new connections.
func (g *tcpGate) setBlocked(blocked bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.blocked = blocked
	if blocked {
		for conn := range g.connections {
			_ = conn.Close()
		}
	}
}

// close releases the listener and all connections, then joins every worker.
func (g *tcpGate) close() {
	g.mu.Lock()
	g.stopped = true
	_ = g.listener.Close()
	for conn := range g.connections {
		_ = conn.Close()
	}
	g.mu.Unlock()
	g.wg.Wait()
}

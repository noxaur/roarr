package httpx

import (
	"context"
	"net"
	"testing"
)

func TestIsLoopback(t *testing.T) {
	for h, want := range map[string]bool{"localhost": true, "127.0.0.1": true, "[::1]": true, "app.localhost": true, "sonarr": false, "192.168.1.2": false} {
		if IsLoopback(h) != want {
			t.Errorf("IsLoopback(%q) = %v", h, !want)
		}
	}
}

// A loopback dial that fails inside a container is retried on the host.
// Here "the host" is 127.0.0.1 and the configured address is [::1], where
// nothing listens.
func TestDialFallsBackToDockerHost(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	addr := net.JoinHostPort("::1", port)
	if c, err := dialer.DialContext(context.Background(), "tcp", addr); err == nil {
		c.Close()
		t.Skip("something listens on [::1] too")
	}
	t.Setenv("DOCKER_HOST_ADDR", "127.0.0.1")
	prev := inContainerFn
	inContainerFn = func() bool { return true }
	defer func() { inContainerFn = prev }()

	c, err := dialContext(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatalf("fallback dial failed: %v", err)
	}
	c.Close()
	if alt, _ := loopbackAlt.Load(addr); alt != net.JoinHostPort("127.0.0.1", port) {
		t.Fatalf("remembered alt = %v", alt)
	}

	inContainerFn = func() bool { return false }
	loopbackAlt.Delete(addr)
	if _, err := dialContext(context.Background(), "tcp", addr); err == nil {
		t.Fatal("outside a container the loopback address must be used as is")
	}
}

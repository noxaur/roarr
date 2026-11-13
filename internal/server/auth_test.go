package server

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name, peer, xff, trusted, want string
	}{
		{"untrusted peer ignores the header", "203.0.113.9:5000", "1.2.3.4", "", "203.0.113.9"},
		{"untrusted peer with a list", "203.0.113.9:5000", "1.2.3.4", "10.0.0.0/8", "203.0.113.9"},
		{"trusted proxy names the client", "10.0.0.2:5000", "198.51.100.7", "10.0.0.0/8", "198.51.100.7"},
		{"forged hop left of the real client", "10.0.0.2:5000", "1.2.3.4, 198.51.100.7", "10.0.0.2", "198.51.100.7"},
		{"chain of trusted proxies", "10.0.0.2:5000", "198.51.100.7, 10.0.0.3", "10.0.0.0/8", "198.51.100.7"},
		{"trusted proxy without header", "10.0.0.2:5000", "", "10.0.0.2", "10.0.0.2"},
		{"ipv6 peer", "[::1]:5000", "198.51.100.7", "::1", "198.51.100.7"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/api/login", nil)
		r.RemoteAddr = c.peer
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := clientIP(r, c.trusted); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestThrottleIgnoresForgedForwardedFor(t *testing.T) {
	a := &auth{failures: map[string][]time.Time{}}
	var err error
	for i := 0; i < 9; i++ {
		r := httptest.NewRequest("POST", "/api/login", nil)
		r.RemoteAddr = "203.0.113.9:5000"
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("1.2.3.%d", i))
		client := clientIP(r, "")
		if err = a.throttle(client); err != nil {
			break
		}
		a.fail(client)
	}
	if err != errThrottled {
		t.Fatalf("a new X-Forwarded-For per attempt must not reset the throttle, got %v", err)
	}
}

func TestThrottleForgetsQuietClients(t *testing.T) {
	a := &auth{failures: map[string][]time.Time{"203.0.113.9": {time.Now().Add(-time.Hour)}}}
	if err := a.throttle("203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	if len(a.failures) != 0 {
		t.Fatalf("expired client kept: %v", a.failures)
	}
}

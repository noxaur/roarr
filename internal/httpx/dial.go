package httpx

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// roarr usually runs in a container next to the *arr apps on one machine.
// Inside a container "localhost" is the container itself, yet a URL like
// http://localhost:8989 is exactly what users copy from their browser, what
// Prowlarr stores for host-networked apps, and what Sonarr stores for a
// qBittorrent on the host. When a loopback dial fails inside a container,
// roarr retries the Docker host (host.docker.internal, then the default
// gateway) before giving up. A remembered alternative is used directly.

var dialer = &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}

var (
	loopbackAlt   sync.Map // "localhost:8989" -> "172.17.0.1:8989"
	inContainerFn = sync.OnceValue(detectContainer)
)

func detectContainer() bool {
	if v := strings.ToLower(os.Getenv("ROARR_IN_DOCKER")); v != "" {
		return v == "1" || v == "true" || v == "yes"
	}
	for _, f := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(f); err == nil {
			return true
		}
	}
	return false
}

// IsLoopback reports whether host names this machine (or container).
func IsLoopback(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// hostCandidates lists addresses that reach the Docker host from inside a
// container. DOCKER_HOST_ADDR overrides detection.
func hostCandidates() []string {
	if v := strings.TrimSpace(os.Getenv("DOCKER_HOST_ADDR")); v != "" {
		return []string{v}
	}
	out := []string{"host.docker.internal", "host.containers.internal"}
	if gw := defaultGateway(); gw != "" {
		out = append(out, gw)
	}
	return out
}

// defaultGateway reads the IPv4 default route from /proc/net/route (Linux).
func defaultGateway() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 3 || fs[1] != "00000000" {
			continue
		}
		b, err := hex.DecodeString(fs[2])
		if err != nil || len(b) != 4 {
			continue
		}
		var ip [4]byte
		binary.LittleEndian.PutUint32(ip[:], binary.BigEndian.Uint32(b))
		return net.IP(ip[:]).String()
	}
	return ""
}

func dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if alt, ok := loopbackAlt.Load(addr); ok {
		if c, err := dialer.DialContext(ctx, network, alt.(string)); err == nil {
			return c, nil
		}
		loopbackAlt.Delete(addr)
	}
	conn, err := dialer.DialContext(ctx, network, addr)
	if err == nil || ctx.Err() != nil || !inContainerFn() {
		return conn, err
	}
	host, port, splitErr := net.SplitHostPort(addr)
	if splitErr != nil || !IsLoopback(host) {
		return conn, err
	}
	for _, h := range hostCandidates() {
		alt := net.JoinHostPort(h, port)
		if c, e := dialer.DialContext(ctx, network, alt); e == nil {
			loopbackAlt.Store(addr, alt)
			return c, nil
		}
	}
	return nil, err
}

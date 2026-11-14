package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const cookieName = "roarr_session"

type session struct {
	User  string `json:"u"`
	Admin bool   `json:"a"`
	Exp   int64  `json:"e"`
}

// auth signs and verifies stateless session cookies, so sessions survive
// restarts and updates (the secret lives in the data volume).
type auth struct {
	secret []byte

	mu       sync.Mutex
	failures map[string][]time.Time
}

func newAuth(dataDir string) (*auth, error) {
	p := filepath.Join(dataDir, "session.key")
	b, err := os.ReadFile(p)
	if err != nil || len(b) < 32 {
		b = make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, b, 0o600); err != nil {
			return nil, err
		}
	}
	return &auth{secret: b, failures: map[string][]time.Time{}}, nil
}

func (a *auth) sign(s session) string {
	payload, _ := json.Marshal(s)
	p := base64.RawURLEncoding.EncodeToString(payload)
	m := hmac.New(sha256.New, a.secret)
	m.Write([]byte(p))
	return p + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (a *auth) verify(v string) (session, bool) {
	p, sig, ok := strings.Cut(v, ".")
	if !ok {
		return session{}, false
	}
	m := hmac.New(sha256.New, a.secret)
	m.Write([]byte(p))
	want := m.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(want, got) {
		return session{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return session{}, false
	}
	var s session
	if json.Unmarshal(raw, &s) != nil || time.Now().Unix() > s.Exp {
		return session{}, false
	}
	return s, true
}

func (a *auth) issue(w http.ResponseWriter, r *http.Request, user string, admin bool) {
	exp := time.Now().Add(30 * 24 * time.Hour)
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: a.sign(session{User: user, Admin: admin, Exp: exp.Unix()}), Path: "/",
		Expires: exp, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r),
	})
}

func (a *auth) clear(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r)})
}

func (a *auth) current(r *http.Request) (session, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return session{}, false
	}
	return a.verify(c.Value)
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

var errThrottled = errors.New("too many attempts, wait a minute")

// throttle allows 8 failed logins per client per 5 minutes.
func (a *auth) throttle(client string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	cut := time.Now().Add(-5 * time.Minute)
	keep := a.failures[client][:0]
	for _, t := range a.failures[client] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		delete(a.failures, client)
		return nil
	}
	a.failures[client] = keep
	if len(keep) >= 8 {
		return errThrottled
	}
	return nil
}

func (a *auth) fail(client string) {
	a.mu.Lock()
	a.failures[client] = append(a.failures[client], time.Now())
	a.mu.Unlock()
}

// clientIP is the peer address, unless the peer is a trusted proxy: then it
// is the nearest X-Forwarded-For hop that is not one. trusted is a comma
// separated list of IPs and CIDRs; anyone else can forge the header.
func clientIP(r *http.Request, trusted string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	nets := parseTrusted(trusted)
	if !isTrusted(host, nets) {
		return host
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		h := strings.TrimSpace(hops[i])
		if h == "" {
			continue
		}
		if !isTrusted(h, nets) {
			return h
		}
		host = h
	}
	return host
}

func parseTrusted(list string) []*net.IPNet {
	var out []*net.IPNet
	for _, f := range strings.Split(list, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !strings.Contains(f, "/") {
			if ip := net.ParseIP(f); ip != nil {
				bits := 128
				if ip.To4() != nil {
					bits = 32
				}
				f += "/" + strconv.Itoa(bits)
			}
		}
		if _, n, err := net.ParseCIDR(f); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func isTrusted(host string, nets []*net.IPNet) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func passwordOK(given, want string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(given), []byte(want)) == 1
}

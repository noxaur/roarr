package importer

import (
	"errors"
	"fmt"
	"strings"
)

// PathRule rewrites one path prefix: what qBittorrent reports (Remote) to
// where roarr sees the same folder (Local).
type PathRule struct {
	Remote string `json:"remote"`
	Local  string `json:"local"`
}

// PathMap is a set of rules; the longest matching prefix wins.
type PathMap []PathRule

// ParsePathMap reads "remote=local" pairs separated by commas or
// semicolons, e.g. "/downloads=/data/downloads,D:\Torrents=/mnt/torrents".
// A pair it cannot read is left out and named in the error; the map still
// holds the others.
func ParsePathMap(s string) (PathMap, error) {
	var m PathMap
	var err error
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		remote, local, ok := strings.Cut(part, "=")
		remote, local = strings.TrimSpace(remote), strings.TrimSpace(local)
		if !ok || remote == "" || local == "" {
			err = errors.Join(err, fmt.Errorf("path mapping %q: want remote=local", part))
			continue
		}
		m = append(m, PathRule{Remote: remote, Local: local})
	}
	return m, err
}

// String renders the map in ParsePathMap's format.
func (m PathMap) String() string {
	parts := make([]string, len(m))
	for i, r := range m {
		parts[i] = r.Remote + "=" + r.Local
	}
	return strings.Join(parts, ",")
}

// Map rewrites p with the longest matching rule. Windows separators in the
// remote path are accepted; the result uses forward slashes after the
// prefix. Paths no rule matches are returned unchanged.
func (m PathMap) Map(p string) string {
	out, _ := m.Match(p)
	return out
}

// Match is Map that also reports whether a rule matched, for callers that
// fall back to another map.
func (m PathMap) Match(p string) (string, bool) {
	pp := strings.ReplaceAll(p, `\`, "/")
	best, bestLen := -1, -1
	for i, r := range m {
		rp := strings.TrimRight(strings.ReplaceAll(r.Remote, `\`, "/"), "/")
		match := pp == rp || strings.HasPrefix(pp, rp+"/") || rp == ""
		if strings.Contains(rp, ":") { // Windows drive letters are case-insensitive
			match = strings.EqualFold(pp, rp) || (len(pp) > len(rp) && strings.EqualFold(pp[:len(rp)], rp) && pp[len(rp)] == '/')
		}
		if match && len(rp) > bestLen {
			best, bestLen = i, len(rp)
		}
	}
	if best < 0 {
		return p, false
	}
	return strings.TrimRight(m[best].Local, `/\`) + pp[bestLen:], true
}

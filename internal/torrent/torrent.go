// Package torrent reads the bits of a .torrent file roarr cares about: the
// info-hash (to track the download in qBittorrent) and the file list.
package torrent

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"strconv"
)

var errBad = errors.New("malformed torrent")

// InfoHash returns the lowercase hex v1 info-hash of a .torrent file.
func InfoHash(b []byte) (string, error) {
	if len(b) == 0 || b[0] != 'd' {
		return "", errBad
	}
	i := 1
	for i < len(b) && b[i] != 'e' {
		key, next, err := readString(b, i)
		if err != nil {
			return "", err
		}
		end, err := skip(b, next)
		if err != nil {
			return "", err
		}
		if key == "info" {
			sum := sha1.Sum(b[next:end])
			return hex.EncodeToString(sum[:]), nil
		}
		i = end
	}
	return "", errors.New("torrent has no info dictionary")
}

func readString(b []byte, i int) (string, int, error) {
	j := i
	for j < len(b) && b[j] != ':' {
		if b[j] < '0' || b[j] > '9' {
			return "", 0, errBad
		}
		j++
	}
	if j >= len(b) {
		return "", 0, errBad
	}
	n, err := strconv.Atoi(string(b[i:j]))
	if err != nil || n < 0 || j+1+n > len(b) {
		return "", 0, errBad
	}
	return string(b[j+1 : j+1+n]), j + 1 + n, nil
}

// skip returns the index just past the bencoded value starting at i.
func skip(b []byte, i int) (int, error) {
	if i >= len(b) {
		return 0, errBad
	}
	switch c := b[i]; {
	case c == 'i':
		for j := i + 1; j < len(b); j++ {
			if b[j] == 'e' {
				return j + 1, nil
			}
		}
		return 0, errBad
	case c == 'l' || c == 'd':
		j := i + 1
		for j < len(b) && b[j] != 'e' {
			var err error
			j, err = skip(b, j)
			if err != nil {
				return 0, err
			}
		}
		if j >= len(b) {
			return 0, errBad
		}
		return j + 1, nil
	case c >= '0' && c <= '9':
		_, next, err := readString(b, i)
		return next, err
	}
	return 0, errBad
}

package torrent

import (
	"crypto/sha1"
	"encoding/hex"
	"testing"
)

func TestInfoHash(t *testing.T) {
	info := "d6:lengthi12e4:name5:a.mkv12:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaae"
	raw := "d8:announce20:http://t/announce/ab4:info" + info + "e"
	sum := sha1.Sum([]byte(info))
	got, err := InfoHash([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if want := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if _, err := InfoHash([]byte("not a torrent")); err == nil {
		t.Fatal("expected error")
	}
}

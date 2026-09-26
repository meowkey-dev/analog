package chartlib

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestPinnedBundle(t *testing.T) {
	digest := sha256.Sum256(Bytes())
	if got := hex.EncodeToString(digest[:]); got != SHA256 {
		t.Fatalf("vendor hash = %s, want %s", got, SHA256)
	}
	if got := strings.Count(BootstrapScript(), "const encoded ="); got != 1 {
		t.Fatalf("bootstrap has %d library payloads", got)
	}
}

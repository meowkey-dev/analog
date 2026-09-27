package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/meowkey-dev/analog/internal/version"
)

func releaseArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tarball := tar.NewWriter(gz)
	if err := tarball.WriteHeader(&tar.Header{Name: "analog-server", Mode: 0755,
		Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarball.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tarball.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstallVerifiedRelease(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("self-upgrade is supported on Unix release builds")
	}
	oldVersion := version.Version
	version.Version = "0.1.0"
	t.Cleanup(func() { version.Version = oldVersion })
	t.Setenv("ANALOG_UPGRADE_DISABLED", "")
	t.Setenv("INVOCATION_ID", "")
	exe := filepath.Join(t.TempDir(), "analog-server")
	if err := os.WriteFile(exe, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	archive := releaseArchive(t, []byte("new"))
	hash := sha256.Sum256(archive)
	asset := fmt.Sprintf("analog-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	badChecksum := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case "latest":
			_, _ = w.Write([]byte(`{"tag_name":"v0.2.0"}`))
		case asset:
			_, _ = w.Write(archive)
		case "SHA256SUMS":
			want := hex.EncodeToString(hash[:])
			if badChecksum {
				want = fmt.Sprintf("%064x", 0)
			}
			_, _ = fmt.Fprintf(w, "%s  %s\n", want, asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	u := &Updater{client: server.Client(), exe: exe, base: server.URL,
		latestURL: server.URL + "/latest"}
	badChecksum = true
	if _, err := u.Install(context.Background()); err == nil {
		t.Fatal("checksum mismatch was accepted")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Fatalf("binary changed after failed verification: %q", got)
	}
	badChecksum = false
	status, err := u.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.Latest != "0.2.0" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new" {
		t.Fatalf("binary was not replaced: %q", got)
	}
}

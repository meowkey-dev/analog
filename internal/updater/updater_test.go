package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/meowkey-dev/analog/internal/version"
)

func releaseArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tarball := tar.NewWriter(gz)
	if err := tarball.WriteHeader(&tar.Header{Name: "./analog-server", Mode: 0755,
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

func TestOfflineLookupIsCachedAndDoesNotOfferUpgrade(t *testing.T) {
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
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer server.Close()
	u := &Updater{client: server.Client(), exe: exe, latestURL: server.URL}
	for range 2 {
		status, err := u.Status(context.Background())
		if err != nil || !status.Supported || status.Available || status.Reason == "" {
			t.Fatalf("offline status = %+v, err %v", status, err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("release host called %d times, want one cached failure", got)
	}
	if _, err := u.Install(context.Background()); !errors.Is(err, ErrRelease) {
		t.Fatalf("Install with offline lookup: %v, want ErrRelease", err)
	}
}

func TestManagedAppAndUnwritableInstallAreNotOffered(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("self-upgrade is supported on Unix release builds")
	}
	oldVersion := version.Version
	version.Version = "0.1.0"
	t.Cleanup(func() { version.Version = oldVersion })
	t.Setenv("ANALOG_UPGRADE_DISABLED", "")
	t.Setenv("INVOCATION_ID", "")
	appDir := filepath.Join(t.TempDir(), "Analog.app", "Contents", "MacOS")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}
	appExe := filepath.Join(appDir, "analog-server")
	if err := os.WriteFile(appExe, []byte("app"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, reason := (&Updater{exe: appExe}).support(); reason != "This server is part of a desktop app; upgrade the app itself." {
		t.Fatalf("app reason = %q", reason)
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write through directory mode restrictions")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "analog-server")
	if err := os.WriteFile(exe, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
	if _, reason := (&Updater{exe: exe}).support(); reason != "The install directory is not writable; upgrade Analog on the server host." {
		t.Fatalf("unwritable install reason = %q", reason)
	}
}

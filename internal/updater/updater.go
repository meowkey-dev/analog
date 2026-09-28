// Package updater replaces a standalone release binary with a verified newer one.
// Package managers and service supervisors own their installations and are never
// modified here.
package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/meowkey-dev/analog/internal/version"
)

const releases = "https://github.com/meowkey-dev/analog/releases"
const latestAPI = "https://api.github.com/repos/meowkey-dev/analog/releases/latest"

var releaseTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

var ErrRelease = errors.New("release lookup, download or verification failed")
var ErrInstall = errors.New("server executable replacement failed")

type Status struct {
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	Available bool   `json:"available"`
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

type Updater struct {
	client    *http.Client
	exe       string
	base      string
	latestURL string
	mu        sync.Mutex
	cachedTag string
	cachedErr error
	cachedAt  time.Time
}

func New() *Updater {
	return &Updater{client: &http.Client{Timeout: 2 * time.Minute}, base: releases,
		latestURL: latestAPI}
}

func (u *Updater) executable() (string, error) {
	if u.exe != "" {
		return u.exe, nil
	}
	return os.Executable()
}

func (u *Updater) support() (string, string) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return "", "Upgrade with the installer for your platform."
	}
	if os.Getenv("ANALOG_UPGRADE_DISABLED") == "1" {
		return "", "Upgrade is disabled by the server operator."
	}
	if !releaseTag.MatchString("v" + version.Version) {
		return "", "This is a development build; build or install a release to upgrade."
	}
	if os.Getenv("INVOCATION_ID") != "" {
		return "", "This server is managed by systemd; upgrade it on the host and restart the service."
	}
	exe, err := u.executable()
	if err != nil {
		return "", err.Error()
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err.Error()
	}
	if strings.Contains(resolved, "/nix/store/") {
		return "", "This server is managed by Nix; update its flake or package."
	}
	if strings.Contains(resolved, "/Cellar/") || strings.Contains(resolved, "/homebrew/") {
		return "", "This server is managed by Homebrew; run brew upgrade analog."
	}
	if strings.Contains(resolved, ".app/Contents/") || os.Getenv("APPIMAGE") != "" {
		return "", "This server is part of a desktop app; upgrade the app itself."
	}
	if filepath.Base(resolved) != "analog-server" {
		return "", "The running executable is not a standalone analog-server release."
	}
	probe, err := os.CreateTemp(filepath.Dir(resolved), ".analog-upgrade-probe-*")
	if err != nil {
		return "", "The install directory is not writable; upgrade Analog on the server host."
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return resolved, ""
}

func (u *Updater) Status(ctx context.Context) (Status, error) {
	status := Status{Current: version.Version}
	_, reason := u.support()
	if reason != "" {
		status.Reason = reason
		return status, nil
	}
	status.Supported = true
	tag, err := u.latest(ctx)
	if err != nil {
		status.Reason = "Could not reach the release host."
		return status, nil
	}
	status.Latest = strings.TrimPrefix(tag, "v")
	status.Available = newer(status.Latest, status.Current)
	return status, nil
}

func newer(latest, current string) bool {
	var a, b [3]int
	_, _ = fmt.Sscanf(latest, "%d.%d.%d", &a[0], &a[1], &a[2])
	_, _ = fmt.Sscanf(current, "%d.%d.%d", &b[0], &b[1], &b[2])
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func (u *Updater) latest(ctx context.Context) (string, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.cachedTag != "" && time.Since(u.cachedAt) < 5*time.Minute {
		return u.cachedTag, nil
	}
	if u.cachedErr != nil && time.Since(u.cachedAt) < time.Minute {
		return "", u.cachedErr
	}
	tag, err := u.fetchLatest(ctx)
	u.cachedAt = time.Now()
	if err != nil {
		u.cachedTag = ""
		u.cachedErr = err
		log.Printf("analog upgrade: release lookup failed: %v", err)
		return "", err
	}
	u.cachedTag = tag
	u.cachedErr = nil
	return tag, nil
}

func (u *Updater) fetchLatest(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.latestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := u.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub release lookup returned HTTP %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", err
	}
	if !releaseTag.MatchString(body.TagName) {
		return "", fmt.Errorf("unexpected release tag %q", body.TagName)
	}
	return body.TagName, nil
}

// Install checks the release and atomically replaces the executable. The caller
// must restart after answering the HTTP request; the current process is still
// running the old inode until then.
func (u *Updater) Install(ctx context.Context) (Status, error) {
	status, err := u.Status(ctx)
	if err != nil || !status.Supported {
		return status, err
	}
	if status.Reason != "" {
		return status, fmt.Errorf("%w: %s", ErrRelease, status.Reason)
	}
	if !status.Available {
		return status, nil
	}
	exe, reason := u.support()
	if reason != "" {
		status.Supported = false
		status.Reason = reason
		return status, nil
	}
	asset := fmt.Sprintf("analog-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	tag := "v" + status.Latest
	archive, err := u.fetch(ctx, u.base+"/download/"+tag+"/"+asset, 100<<20)
	if err != nil {
		return status, fmt.Errorf("%w: %v", ErrRelease, err)
	}
	checksums, err := u.fetch(ctx, u.base+"/download/"+tag+"/SHA256SUMS", 1<<20)
	if err != nil {
		return status, fmt.Errorf("%w: %v", ErrRelease, err)
	}
	want, err := checksumFor(checksums, asset)
	if err != nil {
		return status, fmt.Errorf("%w: %v", ErrRelease, err)
	}
	have := sha256.Sum256(archive)
	if hex.EncodeToString(have[:]) != want {
		return status, fmt.Errorf("%w: archive checksum mismatch", ErrRelease)
	}
	binary, err := serverFromArchive(archive)
	if err != nil {
		return status, fmt.Errorf("%w: %v", ErrRelease, err)
	}
	if err := replace(exe, binary); err != nil {
		return status, fmt.Errorf("%w: %v", ErrInstall, err)
	}
	return status, nil
}

func (u *Updater) fetch(ctx context.Context, address string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release download returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("release download exceeds size limit")
	}
	return data, nil
}

func checksumFor(data []byte, name string) (string, error) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name && len(fields[0]) == 64 {
			if _, err := hex.DecodeString(fields[0]); err == nil {
				return strings.ToLower(fields[0]), nil
			}
		}
	}
	return "", fmt.Errorf("release checksum for %s is missing", name)
}

func serverFromArchive(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tarball := tar.NewReader(gz)
	for {
		header, err := tarball.Next()
		if err == io.EOF {
			return nil, errors.New("release archive has no analog-server binary")
		}
		if err != nil {
			return nil, err
		}
		if (header.Name == "analog-server" || header.Name == "./analog-server") && header.Typeflag == tar.TypeReg {
			if header.Size <= 0 || header.Size > 30<<20 {
				return nil, errors.New("release server binary has an unexpected size")
			}
			return io.ReadAll(io.LimitReader(tarball, header.Size))
		}
	}
}

func replace(exe string, data []byte) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".analog-server-upgrade-*")
	if err != nil {
		return fmt.Errorf("cannot write beside analog-server: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Chmod(0755); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), exe)
}

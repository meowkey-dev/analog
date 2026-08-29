package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/meowkey-dev/analog/internal/apierr"
	"github.com/meowkey-dev/analog/internal/config"
	"github.com/meowkey-dev/analog/internal/ids"
)

// mediaNameRE is what a stored filename may look like. Server-assigned names always
// match; a client-supplied one is never trusted this far.
var mediaNameRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// Media is what POST /media returns.
type Media struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Bytes       int    `json:"bytes"`
}

// SaveMedia writes an upload under media/<space_id>/<m_ulid>.<ext>.
//
// Keyed by space id, not slug, so renaming a space cannot orphan its files. The
// client's filename is advisory and never touches the filesystem: the stored name is
// server-assigned, so a traversal attempt has nothing to traverse.
func (s *Store) SaveMedia(slug, contentType string, data []byte) (Media, error) {
	space, err := s.spaceRow(s.read, slug)
	if err != nil {
		return Media{}, err
	}
	base := strings.TrimSpace(strings.Split(contentType, ";")[0])
	suffix, ok := config.MediaExtensions[base]
	if !ok {
		return Media{}, apierr.UnsupportedKind(
			fmt.Sprintf("unsupported content type '%s'", contentType),
			apierr.Detail{"supported": config.SupportedMediaTypes()})
	}
	if len(data) > config.MaxUploadBytes {
		return Media{}, apierr.ValidationFailed(
			fmt.Sprintf("upload exceeds %d bytes", config.MaxUploadBytes))
	}

	name := ids.MediaID() + suffix
	target := filepath.Join(s.MediaRoot, space.ID)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return Media{}, err
	}
	if err := os.WriteFile(filepath.Join(target, name), data, 0o644); err != nil {
		return Media{}, err
	}
	return Media{
		URL:         fmt.Sprintf("%s/spaces/%s/media/%s", config.APIPrefix, slug, name),
		ContentType: contentType,
		Bytes:       len(data),
	}, nil
}

// MediaPath resolves a stored file, and its content type from the extension.
func (s *Store) MediaPath(slug, name string) (string, string, error) {
	space, err := s.spaceRow(s.read, slug)
	if err != nil {
		return "", "", err
	}
	if !mediaNameRE.MatchString(name) || strings.Contains(name, "..") {
		return "", "", apierr.NotFound("no such media")
	}
	path := filepath.Join(s.MediaRoot, space.ID, name)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", "", apierr.NotFound("no such media")
	}
	suffix := strings.ToLower(filepath.Ext(path))
	contentType := "application/octet-stream"
	for ct, ext := range config.MediaExtensions {
		if ext == suffix {
			contentType = ct
			break
		}
	}
	return path, contentType, nil
}

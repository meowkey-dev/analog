package client

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// LoadConfig reads ~/.analog.toml, overridden by ANALOG_URL / ANALOG_ACTOR /
// ANALOG_ACTOR_KIND / ANALOG_WEB_URL / ANALOG_SPACE / ANALOG_TOKEN (SPEC §4.2).
//
// path is "" for the default location.
func LoadConfig(path string) map[string]string {
	config := map[string]string{}
	if path == "" {
		path = os.Getenv("ANALOG_CONFIG")
	}
	if path == "" {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, ".analog.toml")
		}
	}
	if path != "" {
		var raw map[string]any
		if _, err := toml.DecodeFile(path, &raw); err == nil {
			for key, value := range raw {
				// Only scalars: a [section] is not configuration this client reads.
				if s, ok := scalar(value); ok {
					config[key] = s
				}
			}
		}
	}
	for key, env := range map[string]string{
		"url": "ANALOG_URL", "actor": "ANALOG_ACTOR", "actor_kind": "ANALOG_ACTOR_KIND",
		"web_url": "ANALOG_WEB_URL", "space": "ANALOG_SPACE", "token": "ANALOG_TOKEN",
	} {
		if value := os.Getenv(env); value != "" {
			config[key] = value
		}
	}
	return config
}

func scalar(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	case int64:
		return strconv.FormatInt(v, 10), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	}
	return "", false
}

// NormalizeBase makes any spelling of the server address into its /api base.
func NormalizeBase(url string) string {
	url = strings.TrimRight(url, "/")
	if strings.HasSuffix(url, "/api") {
		return url
	}
	return url + "/api"
}

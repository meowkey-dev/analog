package main

import (
	"reflect"
	"testing"
)

func TestTranslate(t *testing.T) {
	cases := []struct {
		name      string
		argv      []string
		binDir    string
		forwarded []string
	}{
		{"plain forward", []string{"shim-agent", "--url", "http://x"},
			"", []string{"shim-agent", "--url", "http://x"}},
		{"--bin-dir consumed, not forwarded", []string{"--bin-dir", "/tmp/bin", "a"},
			"/tmp/bin", []string{"a"}},
		{"--bin-dir= form", []string{"--bin-dir=/tmp/bin", "a"},
			"/tmp/bin", []string{"a"}},
		{"bare --wrapper gains the default", []string{"a", "--wrapper", "--issue"},
			"", []string{"a", "--wrapper", "~/.local/bin", "--issue"}},
		{"bare --claude-env gains the default", []string{"a", "--claude-env"},
			"", []string{"a", "--claude-env", "."}},
		{"valued --wrapper passes through", []string{"a", "--wrapper", "/x"},
			"", []string{"a", "--wrapper", "/x"}},
		{"--claude-env before a non-flag passes both tokens through",
			[]string{"--claude-env", "a"},
			"", []string{"--claude-env", "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			binDir, forwarded, err := translate(tc.argv)
			if err != nil {
				t.Fatalf("translate(%q): %v", tc.argv, err)
			}
			if binDir != tc.binDir {
				t.Errorf("binDir = %q, want %q", binDir, tc.binDir)
			}
			if !reflect.DeepEqual(forwarded, tc.forwarded) {
				t.Errorf("forwarded = %q, want %q", forwarded, tc.forwarded)
			}
		})
	}
}

func TestTranslateMissingBinDirValue(t *testing.T) {
	if _, _, err := translate([]string{"--bin-dir"}); err == nil {
		t.Error("expected an error for --bin-dir with no value")
	}
}

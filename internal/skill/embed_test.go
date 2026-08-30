package skill

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The embedded skill and the canonical skill/analog/ at the repo root must say the
// same thing. The subcommand teaches the embedded one; a checkout that edits only
// skill/analog/ would ship a skill that disagrees with the binary — exactly the
// drift embedding exists to prevent.
func TestEmbeddedSkillMatchesSkillDir(t *testing.T) {
	canonical := filepath.Join("..", "..", "skill", "analog")
	entries, err := os.ReadDir(canonical)
	if err != nil {
		t.Fatal(err)
	}

	embedded := FS()
	for _, e := range entries {
		if e.IsDir() {
			t.Fatalf("unexpected directory in skill/analog: %s", e.Name())
		}
		want, err := os.ReadFile(filepath.Join(canonical, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		got, err := fs.ReadFile(embedded, e.Name())
		if err != nil {
			t.Fatalf("skill/analog/%s is not embedded: %v", e.Name(), err)
		}
		if string(got) != string(want) {
			t.Errorf("skill/analog/%s and the embedded copy have drifted", e.Name())
		}
	}

	// And nothing embedded that the canonical copy does not know about.
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Name()] = true
	}
	err = fs.WalkDir(embedded, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !seen[filepath.Base(path)] {
			t.Errorf("embedded %s has no counterpart in skill/analog/", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

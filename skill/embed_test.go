package skill

import (
	"io/fs"
	"testing"
)

func TestCanonicalSkillIsEmbedded(t *testing.T) {
	for _, name := range []string{"SKILL.md", "STYLE.md", "EXPLAIN.md"} {
		body, err := fs.ReadFile(FS(), name)
		if err != nil {
			t.Fatalf("read embedded %s: %v", name, err)
		}
		if len(body) == 0 {
			t.Errorf("embedded %s is empty", name)
		}
	}
}

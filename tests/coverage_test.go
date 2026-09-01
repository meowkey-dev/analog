// Self-coverage: the suite must reference every operation in openapi.json and
// every fixture in contracts/fixtures/.
//
// While the python harness existed, this test enforced coverage parity between
// the two suites (issue #58); it now keeps this suite honest on its own. A new
// endpoint or fixture with no test exercising it is a red build, not a quiet gap.
package conformance

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func suiteSources(t *testing.T) map[string]string {
	t.Helper()
	sources := map[string]string{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".go") {
			raw, err := os.ReadFile(entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			sources[entry.Name()] = string(raw)
		}
	}
	return sources
}

func TestCoverage_EveryOpenapiOperationIsReferenced(t *testing.T) {
	sources := suiteSources(t)

	for path, ops := range asMap(openapiDoc(t)["paths"]) {
		for method := range asMap(ops) {
			if method == "parameters" {
				continue
			}
			t.Run(method+" "+path, func(t *testing.T) {
				// The openapi path /spaces/{slug}/cards appears in test sources as
				// /api/spaces/<real id>/cards; match the shape, not the ids. A
				// trailing id is often concatenated (`"...links/" + id`), so the
				// last placeholder runs to any non-slash rather than to a quote.
				segments := strings.Split(path, "/")
				for i, segment := range segments {
					if strings.HasPrefix(segment, "{") {
						segments[i] = `[^/"]+`
						if i == len(segments)-1 {
							segments[i] = `[^/]+`
						}
					}
				}
				re := regexp.MustCompile("/api" + strings.Join(segments, "/"))
				found := false
				for _, src := range sources {
					if re.FindString(src) != "" {
						found = true
					}
				}
				if !found {
					t.Errorf("the suite never exercises %s %s", method, path)
				}
			})
		}
	}
}

func TestCoverage_EveryFixtureIsReferenced(t *testing.T) {
	sources := suiteSources(t)

	fixtures, err := os.ReadDir(filepath.Join(repoRoot, "contracts", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		t.Run(f.Name(), func(t *testing.T) {
			name := f.Name()
			found := false
			for _, src := range sources {
				if strings.Contains(src, name) {
					found = true
				}
			}
			if !found {
				t.Errorf("the suite never reads fixtures/%s", name)
			}
		})
	}
}

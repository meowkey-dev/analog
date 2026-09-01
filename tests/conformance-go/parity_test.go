// Parity between the python harness and this suite (issue #58).
//
// While both suites exist, they must check the same thing. The dual CI run
// catches a suite that stops passing; this file catches a suite that stops
// *covering*. Every operation in openapi.json and every fixture file has to be
// referenced by both test suites. When the python suite retires, the python half
// of these assertions retires with it.
package conformance

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func conformanceGoSources(t *testing.T) map[string]string {
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

func pythonSources(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join(repoRoot, "tests", "contract")
	sources := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".py") {
			raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			sources[entry.Name()] = string(raw)
		}
	}
	return sources
}

// goSuiteCovers and pySuiteCovers are the switches the retirement flips: phase 2
// deletes the python suite and turns pySuiteCovers into a no-op.
var pySuiteActive = true

func TestParity_EveryOpenapiOperationIsReferencedByBothSuites(t *testing.T) {
	goSources := conformanceGoSources(t)
	pySources := pythonSources(t)

	for path, ops := range asMap(openapiDoc(t)["paths"]) {
		for method := range asMap(ops) {
			if method == "parameters" {
				continue
			}
			t.Run(method+" "+path, func(t *testing.T) {
				// The openapi path /spaces/{slug}/cards appears in test sources as
				// /api/spaces/<real id>/cards; match the shape, not the ids. A
				// trailing id is often concatenated in go (`"...links/" + id`), so
				// the last placeholder runs to any non-slash rather than to a quote.
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
				covered := func(sources map[string]string) bool {
					for _, src := range sources {
						if re.FindString(src) != "" {
							return true
						}
					}
					return false
				}
				if !covered(goSources) {
					t.Errorf("the go suite never exercises %s %s", method, path)
				}
				if pySuiteActive && !covered(pySources) {
					t.Errorf("the python suite never exercises %s %s", method, path)
				}
			})
		}
	}
}

func TestParity_EveryFixtureIsReferencedByBothSuites(t *testing.T) {
	goSources := conformanceGoSources(t)
	pySources := pythonSources(t)

	fixtures, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		t.Run(f.Name(), func(t *testing.T) {
			name := f.Name()
			covered := func(sources map[string]string) bool {
				for _, src := range sources {
					if strings.Contains(src, name) {
						return true
					}
				}
				return false
			}
			if !covered(goSources) {
				t.Errorf("the go suite never reads fixtures/%s", name)
			}
			if pySuiteActive && !covered(pySources) {
				t.Errorf("the python suite never reads fixtures/%s", name)
			}
		})
	}
}

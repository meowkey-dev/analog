// The conformance suite must not know what language the server is written in.
//
// The go module boundary already makes the implementation unimportable — this
// module is outside github.com/meowkey-dev/analog — but the rule is asserted
// rather than trusted: a require, a replace, or an accidentally shared module
// would quietly hand the judge the defendant's own objects.
package conformance

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const analogModule = "github.com/meowkey-dev/analog"

func TestBlackBox_NoAnalogModule(t *testing.T) {
	// go.mod must not require or replace the implementation's module.
	raw, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, analogModule) {
			t.Errorf("go.mod references the implementation's module: %s", line)
		}
	}
}

func TestBlackBox_NoAnalogPackagesInTestBinary(t *testing.T) {
	// The full dependency graph of the test binary, including test-only deps,
	// must not contain a single package from the implementation.
	cmd := exec.Command("go", "list", "-deps", "-test", "./...")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == analogModule || strings.HasPrefix(line, analogModule+"/") {
			t.Errorf("the suite depends on %s", line)
		}
	}
}

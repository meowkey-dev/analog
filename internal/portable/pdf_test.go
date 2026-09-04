package portable

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCompletePDFRejectsAHeaderOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := completePDF(path); ok {
		t.Fatal("header-only output was accepted as a complete PDF")
	}
}

func TestCompletePDFRequiresCrossReferenceAndEOF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "complete.pdf")
	pdf := validTestPDF()
	if err := os.WriteFile(path, []byte(pdf), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := completePDF(path); !ok || string(got) != pdf {
		t.Fatalf("completePDF = %t, %q", ok, got)
	}

	if err := os.WriteFile(path, []byte(strings.Replace(pdf, "%%EOF", "", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := completePDF(path); ok {
		t.Fatal("output without the EOF marker was accepted")
	}
}

func TestRunPrintToPDFWaitsForStableCompleteOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake Chrome uses a POSIX shell")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-chrome")
	contents := `#!/bin/sh
out=""
for arg in "$@"; do
  case "$arg" in
    --print-to-pdf=*) out="${arg#--print-to-pdf=}" ;;
  esac
done
printf '%%PDF-1.7\n' > "$out"
sleep 0.35
printf '1 0 obj\n<< /Type /Catalog >>\nendobj\nxref\n0 2\n0000000000 65535 f \n0000000009 00000 n \ntrailer\n<< /Root 1 0 R >>\nstartxref\n45\n%%%%EOF\n' >> "$out"
sleep 2
`
	if err := os.WriteFile(script, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "board.pdf")
	cmd := exec.Command(script, "--print-to-pdf="+out)
	if _, err := runPrintToPDF(cmd, out, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, ok := completePDF(out); !ok {
		data, _ := os.ReadFile(out)
		t.Fatalf("runPrintToPDF accepted incomplete output: %q", data)
	}
}

func TestPDFKeepsChromeSandboxEnabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake Chrome uses a POSIX shell")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-chrome")
	argsFile := filepath.Join(dir, "args")
	contents := `#!/bin/sh
out=""
for arg in "$@"; do
  printf '%s\n' "$arg" >> "$ANALOG_ARGS_FILE"
  case "$arg" in
    --print-to-pdf=*) out="${arg#--print-to-pdf=}" ;;
  esac
done
printf '%%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\nendobj\nxref\n0 2\n0000000000 65535 f \n0000000009 00000 n \ntrailer\n<< /Root 1 0 R >>\nstartxref\n45\n%%%%EOF\n' > "$out"
sleep 2
`
	if err := os.WriteFile(script, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANALOG_CHROME", script)
	t.Setenv("ANALOG_ARGS_FILE", argsFile)
	if _, err := PDF("<!doctype html><p>safe</p>"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(args), "--no-sandbox") {
		t.Fatal("PDF export disabled Chrome's sandbox")
	}
}

func validTestPDF() string {
	return "%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog >>\nendobj\n" +
		"xref\n0 2\n0000000000 65535 f \n0000000009 00000 n \n" +
		"trailer\n<< /Root 1 0 R >>\nstartxref\n45\n%%EOF\n"
}

package portable

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

const stablePDFObservations = 3

// PDF prints HTML to PDF through a local Chrome/Chromium. analog-server stays
// CGO-free and under 20 MB; a PDF library that could render HTML cards, SVG and
// KaTeX would be neither. The board UI uses the same HTML via window.print().
//
// Chrome can write a complete PDF and then sit there under --headless; once the
// output is structurally complete and stable, it is safe to stop that process.
func PDF(html string) ([]byte, error) {
	bin := ChromePath()
	if bin == "" {
		return nil, fmt.Errorf("pdf export needs Chrome or Chromium (none on PATH); use --format html and Print to PDF, or set ANALOG_CHROME")
	}
	dir, err := os.MkdirTemp("", "analog-export-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	in := filepath.Join(dir, "board.html")
	out := filepath.Join(dir, "board.pdf")
	if err := os.WriteFile(in, []byte(html), 0o600); err != nil {
		return nil, err
	}

	// classic --headless first: --headless=new is flakier under restricted hosts.
	modes := []string{"--headless", "--headless=new"}
	var last string
	for i, mode := range modes {
		_ = os.Remove(out)
		profile := filepath.Join(dir, fmt.Sprintf("chrome-%d", i))
		args := []string{
			mode,
			"--disable-gpu",
			"--disable-extensions",
			"--disable-dev-shm-usage",
			"--disable-background-networking",
			"--no-pdf-header-footer",
			"--no-first-run",
			"--no-default-browser-check",
			"--user-data-dir=" + profile,
			"--print-to-pdf=" + out,
			fileURL(in),
		}
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"HOME="+dir,
			"XDG_CONFIG_HOME="+profile,
			"XDG_CACHE_HOME="+filepath.Join(dir, "cache"),
		)
		output, err := runPrintToPDF(cmd, out, 25*time.Second)
		last = output
		if err == nil {
			break
		}
		if i == len(modes)-1 {
			return nil, fmt.Errorf("chrome print-to-pdf: %v (%s)", err, trim(last, 400))
		}
	}

	pdf, ok := completePDF(out)
	if !ok {
		return nil, fmt.Errorf("chrome wrote an incomplete or invalid pdf")
	}
	return pdf, nil
}

// ChromePath is the first chrome/chromium binary we can run. ANALOG_CHROME wins.
func ChromePath() string {
	if p := os.Getenv("ANALOG_CHROME"); p != "" {
		if abs, err := exec.LookPath(p); err == nil {
			return abs
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return ""
	}
	var names []string
	switch runtime.GOOS {
	case "darwin":
		names = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"google-chrome", "chromium", "chromium-browser",
		}
	case "windows":
		names = []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
			"chrome", "msedge",
		}
	default:
		names = []string{
			"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
			"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser",
		}
	}
	for _, name := range names {
		if filepath.IsAbs(name) {
			if _, err := os.Stat(name); err == nil {
				return name
			}
			continue
		}
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

func runPrintToPDF(cmd *exec.Cmd, out string, d time.Duration) (string, error) {
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	deadline := time.Now().Add(d)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var previous []byte
	stablePolls := 0
	for {
		select {
		case err := <-done:
			if _, ok := completePDF(out); ok && err == nil {
				return buf.String(), nil
			}
			if err != nil {
				return buf.String(), err
			}
			return buf.String(), fmt.Errorf("chrome exited without writing a complete pdf")
		case <-ticker.C:
			pdf, ok := completePDF(out)
			if !ok {
				previous = nil
				stablePolls = 0
			} else if bytes.Equal(pdf, previous) {
				stablePolls++
			} else {
				previous = append(previous[:0], pdf...)
				stablePolls = 1
			}
			// A complete trailer can appear before the writer closes the file.
			// Require several unchanged observations before stopping Chrome, so a
			// transient header/trailer cannot win a race with the final bytes.
			if stablePolls >= stablePDFObservations {
				killTree(cmd)
				<-done
				return buf.String(), nil
			}
			if time.Now().After(deadline) {
				killTree(cmd)
				<-done
				return buf.String(), fmt.Errorf("timed out after %s", d)
			}
		}
	}
}

// completePDF is intentionally a small completion check, not a PDF renderer.
// Chrome's output has a header, a cross-reference section (or stream), a root,
// and a final EOF marker; all of those must be present and the xref offset must
// point inside the file. The caller separately checks that two reads are equal.
func completePDF(path string) ([]byte, bool) {
	pdf, err := os.ReadFile(path)
	if err != nil || len(pdf) < 32 || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		return nil, false
	}
	eof := bytes.LastIndex(pdf, []byte("%%EOF"))
	if eof < 0 || len(bytes.TrimSpace(pdf[eof+len("%%EOF"):])) != 0 {
		return nil, false
	}
	body := pdf[:eof]
	start := bytes.LastIndex(body, []byte("startxref"))
	if start < 0 {
		return nil, false
	}
	fields := bytes.Fields(body[start+len("startxref"):])
	if len(fields) == 0 {
		return nil, false
	}
	offset, err := strconv.ParseInt(string(fields[0]), 10, 64)
	if err != nil || offset < 0 || offset >= int64(eof) {
		return nil, false
	}
	section := bytes.TrimSpace(pdf[offset:eof])
	isXRefTable := bytes.HasPrefix(section, []byte("xref")) && bytes.Contains(body, []byte("trailer"))
	isXRefStream := bytes.Contains(section, []byte(" obj")) && bytes.Contains(section, []byte("/Type /XRef"))
	if !isXRefTable && !isXRefStream {
		return nil, false
	}
	if !bytes.Contains(body, []byte("/Root")) {
		return nil, false
	}
	return pdf, true
}

func fileURL(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if runtime.GOOS == "windows" {
		return "file:///" + filepath.ToSlash(abs)
	}
	return "file://" + abs
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

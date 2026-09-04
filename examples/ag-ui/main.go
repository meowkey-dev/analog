// Command ag-ui-demo serves a tiny CORS-aware AG-UI-shaped sidecar and a browser
// harness that places card.html in the same scripts-only iframe Analog uses.
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"log"
	"net/http"
	"strings"
	"time"
)

//go:embed card.html
var cardHTML string

func main() {
	addr := flag.String("addr", "127.0.0.1:9191", "address for the demo sidecar")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/", harness)
	mux.HandleFunc("/health", health)
	mux.HandleFunc("/agent", agent)

	log.Printf("AG-UI demo sidecar on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func harness(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html>
<meta charset="utf-8">
<title>AG-UI iframe smoke</title>
<style>body{font:14px system-ui,sans-serif;margin:24px}#result{font-weight:650}iframe{width:520px;height:280px;border:1px solid #ccd}</style>
<h1>AG-UI iframe smoke</h1>
<p id="result" data-result="pending">waiting for the card</p>
<pre id="received"></pre>
<iframe id="card" sandbox="allow-scripts" srcdoc="%s"></iframe>
<script>
const frame = document.getElementById("card");
const result = document.getElementById("result");
const received = document.getElementById("received");
addEventListener("message", (event) => {
  if (event.source !== frame.contentWindow || event.data?.type !== "ag-ui-demo-result") return;
  const value = event.data;
  result.dataset.result = value.ok ? "pass" : "fail";
  result.textContent = value.ok
    ? "AG-UI smoke: PASS (origin " + value.origin + ")"
    : "AG-UI smoke: FAIL — " + value.error;
  received.textContent = JSON.stringify(value, null, 2);
});
</script>`, html.EscapeString(cardHTML))
}

func health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func agent(w http.ResponseWriter, r *http.Request) {
	if !corsForOpaqueFrame(w, r) {
		return
	}
	log.Printf("%s /agent origin=%q", r.Method, r.Header.Get("Origin"))
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "AG-UI runs use POST", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "expected application/json", http.StatusBadRequest)
		return
	}
	if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		http.Error(w, "expected text/event-stream", http.StatusBadRequest)
		return
	}
	var input struct {
		ThreadID string         `json:"threadId"`
		RunID    string         `json:"runId"`
		State    map[string]any `json:"state"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Tools          []any          `json:"tools"`
		Context        []any          `json:"context"`
		ForwardedProps map[string]any `json:"forwardedProps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "expected a JSON run input", http.StatusBadRequest)
		return
	}
	if input.ThreadID == "" || input.RunID == "" || input.State == nil ||
		len(input.Messages) == 0 || input.Tools == nil || input.Context == nil || input.ForwardedProps == nil {
		http.Error(w, "full RunAgentInput fields are required", http.StatusBadRequest)
		return
	}
	approved, _ := input.State["approved"].(bool)
	log.Printf("interaction message=%q approved=%t", input.Messages[0].Content, approved)
	echo := fmt.Sprintf("Sidecar received message %q; approved=%t", input.Messages[0].Content, approved)

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is unavailable", http.StatusInternalServerError)
		return
	}
	events := []map[string]any{
		{"type": "RUN_STARTED", "threadId": input.ThreadID, "runId": input.RunID},
		{"type": "TEXT_MESSAGE_START", "messageId": "analog-demo-response", "role": "assistant"},
		{"type": "TEXT_MESSAGE_CONTENT", "messageId": "analog-demo-response", "delta": "CORS and streaming worked. "},
		{"type": "TEXT_MESSAGE_CONTENT", "messageId": "analog-demo-response", "delta": echo},
		{"type": "TEXT_MESSAGE_END", "messageId": "analog-demo-response"},
		{"type": "RUN_FINISHED", "threadId": input.ThreadID, "runId": input.RunID},
	}
	for _, event := range events {
		payload, err := json.Marshal(event)
		if err != nil {
			http.Error(w, "could not encode event", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
		// Keep the chunks visibly separate in a real browser smoke test.
		time.Sleep(40 * time.Millisecond)
	}
}

func corsForOpaqueFrame(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") != "null" {
		http.Error(w, "this demo only accepts an opaque-origin sandbox", http.StatusForbidden)
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", "null")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Vary", "Origin")
	return true
}

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestAgentOpaqueOriginPreflight(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/agent", nil)
	req.Header.Set("Origin", "null")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	res := httptest.NewRecorder()
	agent(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", res.Code, http.StatusNoContent)
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "null" {
		t.Fatalf("allow-origin = %q, want null", got)
	}
	if got := res.Header().Get("Access-Control-Allow-Methods"); got != "POST, OPTIONS" {
		t.Fatalf("allow-methods = %q, want POST, OPTIONS", got)
	}
	if got := res.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type" {
		t.Fatalf("allow-headers = %q, want Content-Type", got)
	}

	req = httptest.NewRequest(http.MethodOptions, "/agent", nil)
	req.Header.Set("Origin", "https://example.test")
	res = httptest.NewRecorder()
	agent(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("non-null origin status = %d, want %d", res.Code, http.StatusForbidden)
	}
}

func TestAgentRunStreamsLifecycle(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/agent", strings.NewReader(`{
  "threadId": "thread-test",
  "runId": "run-test",
	  "state": {"approved": true},
  "messages": [{"id": "message-test", "role": "user", "content": "hello"}],
  "tools": [],
  "context": [],
  "forwardedProps": {}
}`))
	req.Header.Set("Origin", "null")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	res := httptest.NewRecorder()
	agent(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want %d", res.Code, http.StatusOK)
	}
	var types []string
	for _, line := range strings.Split(res.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatalf("invalid SSE data %q: %v", line, err)
		}
		types = append(types, event.Type)
	}
	want := []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START",
		"TEXT_MESSAGE_CONTENT",
		"TEXT_MESSAGE_CONTENT",
		"TEXT_MESSAGE_END",
		"RUN_FINISHED",
	}
	if !reflect.DeepEqual(types, want) {
		t.Fatalf("SSE lifecycle = %v, want %v", types, want)
	}
	if body := res.Body.String(); !strings.Contains(body, `Sidecar received message \"hello\"; approved=true`) {
		t.Fatalf("SSE response did not echo the form interaction: %s", body)
	}
}

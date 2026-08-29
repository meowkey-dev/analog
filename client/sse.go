package client

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// forEachSSEMessage parses the SSE framing and hands over one decoded event per
// message. Comment lines (`: keepalive`) are skipped; a message that is not valid
// JSON is dropped rather than killing the stream.
//
// Returns nil at end of stream, so the caller can reconnect.
func forEachSSEMessage(body io.Reader, fn func(Event) error) error {
	scanner := bufio.NewScanner(body)
	// A card's text can be large, and it rides in the event payload.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var data []string
	flush := func() error {
		if len(data) == 0 {
			return nil
		}
		raw := strings.Join(data, "\n")
		data = nil
		var event Event
		dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
		dec.UseNumber()
		if err := dec.Decode(&event); err != nil {
			return nil
		}
		return fn(event)
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		switch {
		case strings.HasPrefix(line, ":"):
			continue
		case line == "":
			if err := flush(); err != nil {
				return err
			}
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimLeft(line[5:], " "))
		}
	}
	return nil
}

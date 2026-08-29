// Command analog-mcp serves SPEC §4.1's ten tools over MCP on stdio.
//
// The transport is newline-delimited JSON-RPC 2.0. Nothing but the framing lives
// here; the tools are in internal/mcp and every rule is in the server.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/meowkey-dev/analog/client"
	"github.com/meowkey-dev/analog/internal/mcp"
)

// protocolVersion is the MCP revision this server speaks.
const protocolVersion = "2024-11-05"

const serverVersion = "0.3.0"

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	api := client.New(client.Options{})
	server := mcp.New(api)

	in := bufio.NewScanner(os.Stdin)
	// Card text rides in tool arguments, and it can be large.
	in.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	out := bufio.NewWriter(os.Stdout)

	for in.Scan() {
		line := in.Bytes()
		if len(line) == 0 {
			continue
		}
		var request message
		if err := json.Unmarshal(line, &request); err != nil {
			write(out, response{JSONRPC: "2.0", ID: json.RawMessage("null"),
				Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		// A notification has no id and takes no reply.
		if len(request.ID) == 0 {
			continue
		}
		write(out, handle(server, request))
	}
}

func write(out *bufio.Writer, r response) {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r); err != nil {
		fmt.Fprintln(os.Stderr, "analog-mcp: encoding response:", err)
	}
	_ = out.Flush()
}

func handle(server *mcp.Server, request message) response {
	reply := response{JSONRPC: "2.0", ID: request.ID}
	switch request.Method {
	case "initialize":
		reply.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "analog", "version": serverVersion},
			"instructions":    mcp.Instructions,
		}
	case "ping":
		reply.Result = map[string]any{}
	case "tools/list":
		reply.Result = map[string]any{"tools": server.Tools()}
	case "tools/call":
		reply.Result = callTool(server, request.Params)
	default:
		reply.Error = &rpcError{Code: -32601, Message: "unknown method: " + request.Method}
	}
	return reply
}

// callTool renders one tool result. A failure comes back as a tool error rather
// than a protocol error: the agent should read the message, not a stack.
func callTool(server *mcp.Server, raw json.RawMessage) map[string]any {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	dec := json.NewDecoder(newReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&params); err != nil {
		return toolError("could not read the tool call: " + err.Error())
	}
	result, err := server.Call(params.Name, params.Arguments)
	if err != nil {
		return toolError(err.Error())
	}

	body, err := json.Marshal(result)
	if err != nil {
		return toolError("could not encode the result: " + err.Error())
	}
	// structuredContent must be an object; anything else is wrapped, which is the
	// shape callers already expect from the Python server.
	structured := map[string]any{}
	if err := json.Unmarshal(body, &structured); err != nil {
		structured = map[string]any{"result": result}
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": string(body)}},
		"structuredContent": structured,
		"isError":           false,
	}
}

func toolError(message string) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": message}},
		"isError": true,
	}
}

func newReader(raw json.RawMessage) io.Reader {
	if len(raw) == 0 {
		return strings.NewReader("{}")
	}
	return strings.NewReader(string(raw))
}

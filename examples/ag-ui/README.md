# AG-UI-shaped HTML card

An Analog HTML card is an isolated document, not an agent runtime. It can still
call an agent sidecar: the browser sends the sandboxed document with the opaque
origin `null`, so the sidecar must answer the CORS preflight and POST with
`Access-Control-Allow-Origin: null`.

`Origin: null` is shared by arbitrary opaque-origin documents; it is a CORS
compatibility value, not an authentication signal. The checked-in demo is
credentialless and loopback-only. Do not put a long-lived bearer token in
stored card HTML. A trusted deployment must arrange user-supplied or
short-lived credentials out of band and authenticate at the sidecar.

The checked-in card uses the same request shape as AG-UI's `HttpAgent`: a JSON
`POST` with `Content-Type: application/json` and `Accept: text/event-stream`,
then reads the response body as an SSE stream. The sidecar intentionally sends
separate chunks and requires `Origin: null`; it also checks the required
`RunAgentInput` fields (`threadId`, `runId`, `state`, `messages`, `tools`,
`context`, and `forwardedProps`).

Run the browser smoke test (Chrome is selected from `ANALOG_CHROME`, or the
standard macOS path):

```bash
scripts/ag-ui-smoke.sh
```

For a manual run, keep the sidecar running and open its harness at
<http://127.0.0.1:9191/>:

```bash
go run ./examples/ag-ui
```

To use the same card in a real canvas, add it while the sidecar is running:

```bash
analog add demo --title "AG-UI card" --kind html --file examples/ag-ui/card.html
```

The main card and its pop-out retain the scripts-only sandbox. HTML/PDF export
also retains that iframe and cannot bundle or proxy the sidecar; an exported
card remains interactive only when its sidecar is still reachable and allows
the opaque origin. The smoke script exits 2 when Chrome is unavailable or
does not finish within its limit, so a browser limitation is reported cleanly
instead of leaving a process behind. Export is a presentation of the canvas,
not a way to run an agent through Analog.

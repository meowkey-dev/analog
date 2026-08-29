# Deploying an Analog server

Loopback with no tokens is the default and is fine on your own machine. Everything
here is for the case where something else needs to reach it.

## The short version

```bash
git clone https://github.com/xukai92/analog && cd analog
uv venv && uv pip install -e .
(cd web && npm ci && npm run build)          # the server serves this
.venv/bin/analog token add kai --kind human  # shown once
.venv/bin/python -m server --host 0.0.0.0
```

The server **refuses to start on a non-loopback address until a token exists**. That
is deliberate: an unauthenticated Analog on a network is world-writable.

## Properly

`analog.service` runs it under systemd bound to loopback, and `Caddyfile` puts TLS
in front. Adapt the domain and paths.

```bash
sudo useradd --system --home /opt/analog analog
sudo mkdir -p /opt/analog /var/lib/analog && sudo chown analog: /opt/analog /var/lib/analog
# clone and build into /opt/analog as above, then:
sudo cp deploy/analog.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now analog
sudo -u analog /opt/analog/.venv/bin/analog token add kai --kind human
```

Only the proxy's port needs to be open. 8787 stays on loopback.

## Tokens

One per actor — you, and each agent:

```bash
analog token add kai --kind human
analog token add claude-code --kind agent
analog token list
analog token revoke codex
```

The server takes `actor` from the token, so the event log's attribution cannot be
claimed by a client. Reissuing revokes the previous token immediately, which is the
whole rotation story today; see AMENDMENTS #10.

Tokens live in `$ANALOG_DATA_DIR/auth.json` as SHA-256 digests, mode 600. Back up
`$ANALOG_DATA_DIR` and you have the canvas, the media and the tokens.

## Connecting

- **Browser**: visit the domain. Same-origin, and it asks for the token itself.
- **Desktop app**: put the URL in the connection screen and paste the token.
- **Agents**: `analog login https://analog.meowkey.com --token analog_...`, or set
  `ANALOG_URL` / `ANALOG_ACTOR` / `ANALOG_TOKEN`.

`ANALOG_CORS_ORIGINS` is only needed if the UI is served from a different origin
than the API. The Tauri app's origin is already allowed.

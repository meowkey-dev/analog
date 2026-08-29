# Deploying an Analog server

Loopback with no tokens is the default and is fine on your own machine. Everything
here is for the case where something else needs to reach it.

## The short version

```bash
# One binary, no runtime to install. Pick your platform from the releases page.
curl -L https://github.com/meowkey-dev/analog/releases/latest/download/analog-darwin-arm64.tar.gz | tar xz
./analog-server token add kai --kind human   # shown once
./analog-server --host 0.0.0.0
```

The web UI is inside `analog-server`; there is nothing else to build or serve.

The server **refuses to start on a non-loopback address until a token exists**. That
is deliberate: an unauthenticated Analog on a network is world-writable.

## Properly

`analog.service` runs it under systemd bound to loopback, and `Caddyfile` puts TLS
in front. Adapt the domain and paths.

```bash
sudo useradd --system --home /opt/analog analog
sudo mkdir -p /opt/analog /var/lib/analog && sudo chown analog: /opt/analog /var/lib/analog
sudo install -o analog -g analog -m 755 analog-server analog /opt/analog/
sudo cp deploy/analog.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now analog
sudo -u analog ANALOG_DATA_DIR=/var/lib/analog /opt/analog/analog-server token add kai --kind human
```

Only the proxy's port needs to be open. 8787 stays on loopback.

## Tokens

One per actor — you, and each agent:

```bash
analog-server token add kai --kind human      # on the server host
analog-server token add claude-code --kind agent
analog-server token list
analog-server token revoke codex
```

`analog token ...` is the same command group, for when only the client is installed
on the box — either way it reads and writes the server's auth file, so it has to run
where the server does.

The server takes `actor` from the token, so the event log's attribution cannot be
claimed by a client. Reissuing revokes the previous token immediately, which is the
whole rotation story today; see AMENDMENTS #10.

Tokens live in `$ANALOG_DATA_DIR/auth.json` as SHA-256 digests, mode 600. Back up
`$ANALOG_DATA_DIR` and you have the canvas, the media and the tokens.

## Connecting

- **Browser**: visit the domain. Same-origin, and it asks for the token itself.
- **A desktop shell**: put the URL in its connection screen and paste the token.
- **Agents**: `analog login https://analog.example.com --token analog_...`, or set
  `ANALOG_URL` / `ANALOG_ACTOR` / `ANALOG_TOKEN`.

`ANALOG_CORS_ORIGINS` is only needed if the UI is served from a different origin
than the API. The `tauri://` origins a desktop shell uses are already allowed.

## Upgrading

Replace the binary and restart:

```bash
sudo systemctl stop analog
sudo install -o analog -g analog -m 755 analog-server /opt/analog/
sudo systemctl start analog
```

The schema in `internal/store/schema.sql` is frozen, so an upgrade never migrates a
database. `$ANALOG_DATA_DIR` carries across untouched.

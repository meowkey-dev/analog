# Contributing

Contributions are welcome, and the fastest way to have one merged is to know two
things about how this repo is put together.

## `contracts/` is frozen

`contracts/` and `internal/store/schema.sql` describe the wire format, and everything else is
generated from or tested against them. Changing one of those files in a pull request
that also changes behaviour is the one thing that will get a change bounced.

If you need something that isn't in the contract, open an issue describing the gap.
An amendment edits `openapi.json`, `schema.sql` and the fixtures **together** and
bumps `info.version`; the process and every applied amendment so far are in
[contracts/README.md](contracts/README.md).

A contract that stops describing the running system is worse than no contract,
because people still trust it.

## Tests are written against the contract, not the implementation

```bash
go test ./...                           # the Go tests
scripts/build.sh                        # the binaries the suite judges

uv venv && uv pip install -r tests/requirements.txt
.venv/bin/python -m pytest              # the conformance suite, over HTTP

(cd web && npm ci && npm run build)     # tsc --noEmit + vite build
(cd web && npm test)                    # web unit tests
```

`tests/contract/` asserts against `contracts/fixtures/` and SPEC.md, and reaches the
server over a socket rather than importing it — so it judges any binary that answers,
and `test_black_box.py` fails the build if that stops being true. If a change makes
one fail, the interesting question is which of the two is wrong — sometimes it is the
fixture, and that is an amendment.

Anything that needs the implementation's own objects is a Go test next to the code.
`tests/README.md` has the split, and the contract a server binary has to honour.

## Sign your commits off

This project uses the [Developer Certificate of Origin](https://developercertificate.org/).
It is one line, and it says you wrote the patch or have the right to submit it:

```bash
git commit -s
```

There is no CLA. The license is Apache-2.0 and stays that way.

## Style

Match the file you are editing — its comment density, its naming, its idioms. Comments
here explain *why*, on the assumption the reader can see *what* for themselves.

## Scope

There is a commercial desktop app built on this core. It is a separate, closed
repository and it reaches this code only through the HTTP API — it does not have a
private fork, and a contribution here is never quietly diverted into it beyond what
Apache-2.0 already permits of anyone. If a feature would help someone running
`analog-server` themselves, it belongs here.

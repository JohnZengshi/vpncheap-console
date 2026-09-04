# vpncheap-console

A small local console that drives the live VPNCheap (sing-box) Clash API. It
lists every node, tests latency, switches the active node and routing mode,
streams live traffic, lists active connections, and can start/stop the macOS
VPN tunnel. It speaks only to localhost.

## Run

```bash
go build -o vpncheap-console ./cmd/vpncheap-console
./vpncheap-console -addr 127.0.0.1:18090 -clash http://127.0.0.1:9090
```

Then open `http://127.0.0.1:18090/`.

Or:

```bash
make run          # build + run, same defaults
make stop         # full shutdown: disconnect tunnel, quit VPNCheap, stop console
./run.sh          # build + run, with VPNCheap/Clash API preflight checks
```

`make test` runs `go vet` and `go test`.

By default the console **auto-starts VPNCheap**: if the Clash API at `-clash`
is not reachable it opens `VPNCheap.app`, connects the tunnel, and polls the
API for up to 30s. The UI shows this progress via `/health`. Disable with
`-autostart=false`.

## Flags

- `-addr` - listen address. Defaults to `127.0.0.1:18090`. Must be loopback.
- `-clash` - local Clash API base URL. Defaults to `http://127.0.0.1:9090`.
- `-autostart` - launch VPNCheap + connect tunnel if the Clash API is down.
  Defaults to `true`.
- `-pidfile` - pidfile path used by `make stop`. Defaults to
  `~/.vpncheap-console.pid`.
- `-labels` - comma-separated sing-box config paths whose outbound tags become
  human-readable node labels. Defaults to auto-detected paths (easy_proxies
  and the SFM sing-box app).

## Endpoints

All `/api/*` requests are reverse-proxied to the local Clash API:

- `GET /api/version` - Clash/sing-box version.
- `GET /api/configs` - current mode and mode-list.
- `PATCH /api/configs` - switch routing mode `{"mode":"Rule|direct|global"}`.
- `GET /api/proxies` - all proxies and the `proxy` selector.
- `PUT /api/proxies/{selector}` - switch active node `{"name":"<tag>"}`.
- `GET /api/proxies/{name}/delay?timeout=5000&url=...` - test one node's delay.
- `GET /api/traffic` - streaming line-delimited JSON of up/down bytes.
- `GET /api/connections` - active connections.
- `DELETE /api/connections/{id}` - close one connection.

Console-added, not proxied to Clash:

- `POST /best` - probe every node in the `proxy` selector concurrently and
  switch to whichever comes back with the lowest delay. Returns
  `{"results":[{"name","delay"}...],"best":{"name","delay"}}`, or
  `{"results":[...],"error":"..."}` if every node failed.
- `GET /labels` - maps each `xboard_*` proxy name to a human-readable label by
  pairing the Clash API's proxy order with a sing-box config's outbound tags.
  `{"mapping":{"xboard_96b35930...":"HK-香港1-官网Vpncheap.io",...}}`. Empty
  mapping when the source config is missing or lengths disagree.

Console-only:

- `GET /health` - launch phase: `{"phase":"launching|ready|degraded","detail":"..."}`.
- `GET /tunnel?action=status|connect|disconnect` - read/control the macOS VPN
  tunnel via `scutil`. The service name is resolved from `scutil --nc list` by
  matching the bundle id `com.vpncheap.macnative`; it is never taken from
  request input. `disconnect` should be confirmed in the UI before firing.

## Security

The underlying Clash API on `127.0.0.1:9090` has **no authentication**. This
console adds none and must never be bound beyond loopback - the `-addr` flag
rejects any non-loopback bind. Nothing is stored on disk and no credential is
read or logged. Node passwords, obfs passwords, and account UUIDs are never
displayed; only the node tag, type, and measured delay are shown.

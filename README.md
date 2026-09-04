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
- `GET /exit` - probes where the current egress IP actually is (via
  `https://ipinfo.io/json`, which exits through the tunnel when connected).
  Lets you verify a node's real exit location instead of trusting its label.
  `{"ip":"...","city":"...","country":"...","org":"...","tunnel":{"state":"..."}}`.

Console-only:

- `GET /health` - launch phase: `{"phase":"launching|ready|degraded","detail":"..."}`.
- `GET /tunnel?action=status|connect|disconnect` - read/control the macOS VPN
  tunnel via `scutil`. The service name is resolved from `scutil --nc list` by
  matching the bundle id `com.vpncheap.macnative`; it is never taken from
  request input. `disconnect` should be confirmed in the UI before firing.
- Switching nodes (`use`) does **not** bounce the tunnel: it PUTs the selector
  then DELETEs `/api/connections`, dropping live keep-alive connections so
  they rebuild through the new node. Bouncing the tunnel via scutil was tried
  and abandoned - macOS NetworkExtension refuses a rapid stop/start, which
  dropped the VPN entirely.

## Security

The underlying Clash API on `127.0.0.1:9090` has **no authentication**. This
console adds none and must never be bound beyond loopback - the `-addr` flag
rejects any non-loopback bind. Nothing is stored on disk and no credential is
read or logged. Node passwords, obfs passwords, and account UUIDs are never
displayed; only the node tag, type, and measured delay are shown.

## Domain-Based Proxy Router

The console can start an optional HTTP/HTTPS proxy port that routes specific
domains through specific VPNCheap nodes. This is designed for 9router or any
tool that needs a local proxy endpoint with per-domain node selection.

### How it works

When the proxy receives a request, it looks up the target domain in a
domain→node mapping, switches VPNCheap's global selector to the mapped node
via the Clash API (`PUT /proxies/proxy`), then forwards the request. The
traffic exits through VPNCheap's TUN, which uses the just-switched selector.
VPNCheap remains the core proxy process — the console only adds a proxy port
and per-request selector switching.

### Flags

- `-proxy` - start the domain proxy router (default: false)
- `-proxy-addr` - listen address for the proxy router (default: 127.0.0.1:2323, loopback only)
- `-proxy-rules` - path to a JSON file with the domain→node mapping
- `-proxy-drop-connections` - drop existing connections after switching selector,
  forcing keep-alive sessions to rebuild through the new node (default: false)

### Rules file format

```json
{
  "fallback": "xboard_96b35930b860b2e5",
  "rules": [
    {"domain": "openai.com", "node": "xboard_4108327100f34551"},
    {"domain": "github.com", "node": "xboard_96b35930b860b2e5"}
  ]
}
```

Domain matching uses suffix match: a rule for `openai.com` matches both
`openai.com` and `chat.openai.com`. Unmapped domains use the `fallback` node.
If `fallback` is empty, unmapped domains skip the selector switch and use
whatever node VPNCheap currently has selected.

### Usage

```bash
# Start console + proxy with rules
./vpncheap-console -proxy -proxy-rules rules.json

# Point 9router or curl at the proxy
curl -x http://127.0.0.1:2323 https://chat.openai.com
```

### API endpoints (on the console web UI server, port 18090)

- `GET /api/proxy/rules` - current domain→node mapping and fallback
- `PUT /api/proxy/rules` - replace the mapping (validates node tags against
  VPNCheap's selector; takes effect immediately)
- `GET /api/proxy/status` - proxy running state, listen address, last node

### Concurrency note

VPNCheap's selector is global — one node at a time for all traffic. The proxy
serializes selector switches with a mutex held only during the switch + dial
window, then releases for data transfer. Established connections survive a
selector switch (sing-box's `interrupt_exist_connections` defaults to false).
Use `-proxy-drop-connections` to force keep-alive connections to rebuild
through the new node after a switch.

### Interaction with /best

The existing `/best` endpoint switches the selector to the lowest-latency
node. If the proxy is running, both compete for the global selector. The
proxy's per-request switch takes precedence for its own requests; `/best` is
a manual one-shot operation. These are compatible but not designed to run
simultaneously.

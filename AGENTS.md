# AGENTS.md

## Cursor Cloud specific instructions

RaftDB (`github.com/ryansenn/ryanDB`) is a self-contained Go project — an educational Raft consensus implementation backing an in-memory key-value store. There are no external services (no DB/cache/broker), no environment variables, and config is via CLI flags only. State persists to local files under `logs/` (`.rlog`, `.meta`), which is gitignored.

Toolchain: Go 1.24.0 (pinned in `go.mod`; the `go` toolchain auto-fetches it on first use). The update script runs `go mod download`.

Standard commands (see `README.md`, `visualizer/README.md`, and `.github/workflows/ci.yml`):
- Build: `go build -o ryanDB .` (binary `ryanDB` is gitignored)
- Lint: no linter configured; use `go vet ./...`
- Unit tests: `go test -race -count=1 -timeout 5m ./core`
- Integration tests: `go test -count=1 -timeout 10m -v ./test` (CI uses `-count=3`; these build the binary and spin up a real 5-node cluster over HTTP 8001-8005 / gRPC 9001-9005)

Running a cluster manually (the actual product): each node needs a free HTTP port (`--port`, e.g. 8001+) and a gRPC port from `--peers` (9001+). A majority of nodes must be up to commit writes; start ≥3. Example:
`./ryanDB --id=node1 --port=8001 --peers=node1=127.0.0.1:9001,node2=127.0.0.1:9002,node3=127.0.0.1:9003 --reset=true`
Use `--reset=false` on restarts to keep persisted logs. HTTP API: `GET /put?key=&value=`, `GET /get?key=`, `GET /status`. Writes sent to a follower are forwarded to the leader over gRPC; give the cluster a second after boot before sending requests.

Optional visualizer (demo only): `go run ./visualizer --no-browser visualizer/scenarios/showcase.json` serves a browser UI on `:8080` and launches its own 5-node cluster on ports 8001-8005/9001-9005. Gotchas: it auto-opens a browser unless `--no-browser` is passed, and it calls `harness.KillPorts` on 8001-8005/9001-9005 at startup — so do not run it alongside a manually started cluster on those ports.

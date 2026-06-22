# RaftDB Visualizer

A scripted demo tool that starts a Raft cluster, runs a JSON scenario, and animates what happens in the browser. You pass a scenario file: the visualizer executes it and you watch the cluster react.

## Quick start

### 60-second showcase (loops)

```bash
go run ./visualizer visualizer/scenarios/showcase.json
```

A choreographed **60-second loop**: boot → writes → **leader dies** → new leader elected → recovery writes → stable → repeat. Scene titles mark each beat; the hub layout keeps all five machines visible at once.

Timeline (approximate):

| Time | Scene |
|------|-------|
| 0–3s | Empty stage → nodes join one by one → leader elected |
| 3–11s | Client writes every 1.5s |
| 11–15s | Leader fails, new election, writes continue |
| ~19s | Failed leader **rejoins** (`restart: killed`) |
| 19–54s | Continuous writes every 1.5s |
| 54–60s | Brief stable beat, loop restarts |

Showcase mode uses exact timing and **continues past missed writes** during leader failure (logged as `(missed)` in the scenario log, animation keeps running).

### Full demo (~2 minutes)

From the repo root:

```bash
go run ./visualizer visualizer/scenarios/demo.json
```

This will:

1. Build `ryanDB` if it is not already present
2. Start a 5-node cluster
3. Open `http://localhost:8080` in your browser
4. Run the scenario step-by-step with slow waits so animations are easy to follow

Press `Ctrl+C` to stop the visualizer and shut down all nodes.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8080` | Port for the visualizer UI |
| `--no-browser` | `false` | Do not auto-open the browser |
| `--binary` | `ryanDB` in repo root | Path to the raftDB binary |
| `--demo` | `true` | Compress long `wait` steps for presentation pacing |

Showcase mode is enabled when the scenario JSON sets `"showcase": true` (staged boot, exact timing).

Example:

```bash
go run ./visualizer --port 3000 --no-browser visualizer/scenarios/demo.json
```

## What you see in the UI

The browser view is passive: you watch, you do not interact.

**Diagram (hub layout)**
- Four followers across the top, **leader in the center** (larger card, green border), **client below**
- Visible leader↔follower links; each machine has its own log strip
- Labeled packets: `write`, `replicate`, `ack`, `vote`: smooth, readable pacing
- Commit fills the committed zone on every node; failures show dashed offline cards

**HUD**
- Commit index, quorum progress, current term
- Showcase mode: scene card + 30s timeline bar (no step counter)

**Event types shown**
- Client request (blue): visualizer sends put/get to a node
- Forward (teal): follower forwards to leader
- Replicate (purple): leader append entries to followers
- Vote (orange): election request or vote granted

## Scenario file format

Scenarios are JSON files with three top-level fields:

```json
{
  "name": "My scenario",
  "nodes": 5,
  "steps": [ ... ]
}
```

| Field | Description |
|-------|-------------|
| `name` | Display name shown in the UI |
| `nodes` | Cluster size (3–9) |
| `steps` | Ordered list of actions |

Each step must have **exactly one** action. Use `"wait"` steps between actions to control pacing: longer waits (e.g. `"5s"`) make animations easier to follow.

### Step types

**Wait**

```json
{ "wait": "5s", "comment": "optional description" }
```

Duration uses Go syntax: `"500ms"`, `"2s"`, `"1m"`.

**Put**

```json
{ "put": { "node": "node3", "key": "foo", "value": "bar" } }
```

Sends a write to the given node's HTTP API. If the node is a follower, the request is forwarded to the leader.

**Get**

```json
{ "get": { "node": "node5", "key": "foo", "expect": "bar" } }
```

Reads a key from the given node. If `expect` is set, the scenario fails when the value does not match.

**Kill**

```json
{ "kill": "node1", "comment": "optional description" }
```

Stops a node's process. The UI shows it as offline.

**Restart**

```json
{ "restart": "node1", "comment": "optional description" }
```

Restart a stopped node with existing logs (`--reset=false`). Use `"killed"` to restart the node most recently stopped by a `kill` step:

```json
{ "restart": "killed" }
```

Set `"loop": true` on a scenario to restart automatically when it finishes.

## Example scenarios

| File | What it demonstrates |
|------|----------------------|
| `scenarios/showcase.json` | **60-second loop**: boot, writes, leader failure, replacement, recovery |
| `scenarios/demo.json` | Full demo (~2 min): writes, leader kill, restarts, catch-up |

## How it works

```
scenario.json → visualizer → ryanDB nodes (HTTP 8001+, gRPC 9001+)
                    ↓
              browser UI (polls visualizer API)
```

The visualizer is the only component that talks to raft nodes. It starts them, executes scenario steps, and polls `/status` and `/events` on each running node. Raft RPC between nodes is observed via each node's event buffer.

## Ports

For a cluster of size `N`:

- HTTP (client API): `8001` – `8000+N`
- gRPC (Raft RPCs): `9001` – `9000+N`
- Visualizer UI: `8080` (or `--port`)

Make sure these ports are free before running a scenario.

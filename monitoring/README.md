# Monitoring

Prometheus + Grafana stack for live Raft metrics. Started automatically by the playground (`go run ./playground`).

## Prerequisite

Docker Desktop must be running.

## Quick start

```bash
go run ./playground
```

The playground UI embeds Grafana panels (proxied at `/grafana/`). Stat numbers above the charts still come from `/api/metrics/live`.

Manual stack:

```bash
docker compose -f monitoring/docker-compose.yml up
```

## URLs

- Playground UI: http://localhost:8080
- Grafana (embedded panels): http://localhost:3000/d/playground-live/playground-live
- Grafana direct: http://localhost:3000
- Prometheus (proxied): http://localhost:8080/prometheus/targets
- Prometheus direct: http://localhost:9090

## Dashboards

- `playground-live` — four timeseries panels embedded in the playground UI
- `raft-playground` — full cluster dashboard (leader, commits, elections, lag)

## Dynamic scrape targets

When you configure or start a cluster, the playground writes `monitoring/targets.json`. Prometheus reloads this file every 5 seconds via file service discovery.

## Metrics reference

See [docs/observability.md](../docs/observability.md).

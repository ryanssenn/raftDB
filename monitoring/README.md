# Monitoring

Prometheus stack for live Raft metrics. Started automatically by the observatory (`go run ./observatory`).

## Prerequisite

Docker Desktop must be running.

## Quick start

```bash
go run ./observatory
```

The observatory UI queries Prometheus via `/api/metrics/live` and shows native charts—no Grafana in the default path.

Manual Prometheus only:

```bash
docker compose -f monitoring/docker-compose.yml up
```

## URLs

- Observatory UI: http://localhost:8080
- Prometheus targets (proxied): http://localhost:8080/prometheus/targets
- Prometheus direct: http://localhost:9090

## Dynamic scrape targets

When you configure or start a cluster, the observatory writes `monitoring/targets.json`. Prometheus reloads this file every 5 seconds via file service discovery.

## Metrics reference

See [docs/observability.md](../docs/observability.md).

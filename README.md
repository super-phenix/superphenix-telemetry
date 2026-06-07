# Superphenix Telemetry Server

A small Go server that receives anonymous usage telemetry from the
Superphenix open-source ecosystem (deployed across many independent
clusters) and re-exposes it as OpenTelemetry metrics over a Prometheus
scrape endpoint.

The design goal is operational humility: clients send the minimum amount
of information that is useful to the project, the server logs only what
it needs to run, and nothing on either side can be used to identify the
operator of a client deployment.

## What it does

- Accepts a small, well-defined JSON document on `POST /vX/telemetry`.
- Validates every field against a strict schema (regex-bounded names,
  capped sizes, finite numeric values, allow-listed metric kinds).
- Records the resulting observations through the OpenTelemetry SDK as
  counters or gauges, namespaced under `superphenix_telemetry_*`.
- Re-exposes them at `GET /metrics` for any Prometheus-compatible
  scraper to consume.
- Throttles abusive clients: 10 requests per hour per source IP by
  default (configurable), with a sliding-window limiter and a
  `Retry-After` header on rejections.
- Logs every request as structured JSON with no IP, no user-agent and
  no body content, only a short anonymous hash, method, path, status
  and duration.

## Privacy model

To protect the privacy of Superphenix users:

- **No raw IP ever touches disk or logs.** The source address is hashed
  with HMAC-SHA256 using a static 32-byte salt. This ensures that the
  same IP always hashes to the same token, allowing for consistent
  rate-limiting and metric aggregation across restarts and multiple
  server instances while still preserving anonymity.
- **No client-supplied identifier is accepted.** The wire schema has no
  `instance_id`, `hostname`, `user`, `cluster` or similar field. The
  only per-request identifier the server ever sees is the source IP,
  and that is used solely as a rate-limit key.
- **Validation errors never echo the client payload.** A 400 response
  identifies the offending field by position (`metrics[3].name: invalid
  format`) but never includes the value, so a malicious intermediate
  proxy cannot use validation responses to amplify identifying data.
- **Label cardinality is bounded by construction.** Metric names, label
  keys and label values must match narrow regexes; at most 8 labels per
  metric and 50 metrics per request are accepted. This prevents a
  client from accidentally - or deliberately - using the label
  dimension to carry a hostname or username.
- **No payload body is ever logged.** The access log is fixed-shape.

## Wire schema

`POST /v1/telemetry` accepts a single JSON document. Only a pre-defined set of
metrics and labels are allowed.

```json
{
  "schema_version": 1,
  "metrics": [
    {
      "name": "controller_info",
      "kind": "gauge",
      "value": 1,
      "labels": { "version": "1.2.3" }
    },
    {
      "name": "az_info",
      "kind": "gauge",
      "value": 1,
      "labels": {
        "topology": "hyperconverged",
        "type": "storage",
        "version": "1.2.3"
      }
    }
  ]
}
```

Constraints:

| Field          | Rule                                                |
|----------------|-----------------------------------------------------|
| schema_version | must equal `1`                                      |
| metrics        | 1–50 entries                                        |
| name           | `controller_info`, `az_info`, `component_info`, `az_count`, `nodes_per_az` |
| kind           | `counter` or `gauge`                                |
| value          | finite float; counters must be ≥ 0                  |
| labels         | 0–8 entries                                         |
| label key      | `^[a-z][a-z0-9_]{0,31}$`                            |
| label value    | `^[A-Za-z0-9._-]{1,64}$`                            |
| body           | ≤ 64 KiB                                            |

Successful submissions return `204 No Content`. Validation failures
return `400 Bad Request` with a short, payload-free explanation.
Over-quota clients receive `429 Too Many Requests` with a `Retry-After`
header in seconds.

The server automatically adds a `hashed_ip` label to every ingested
metric to ensure uniqueness across different client clusters while
preserving anonymity.

The submitted metrics appear in the scrape output prefixed with
`superphenix_telemetry_` - e.g. the example above produces
`superphenix_telemetry_controller_info{version="1.2.3", hashed_ip="..."} 1`.

## Endpoints

| Method | Path             | Purpose                                                             |
|--------|------------------|---------------------------------------------------------------------|
| POST   | `/v1/telemetry`  | Submit a report                                                     |
| GET    | `/metrics`       | Prometheus scrape (server's own metrics + ingested counters/gauges) |
| GET    | `/healthz`       | Liveness probe                                                      |
| GET    | `/readyz`        | Readiness probe                                                     |

## Configuration

All configuration is via environment variables.

| Variable              | Default  | Purpose                                                                                   |
|-----------------------|----------|-------------------------------------------------------------------------------------------|
| `LISTEN_ADDR`         | `:8080`  | Bind address                                                                              |
| `LOG_LEVEL`           | `info`   | `debug`, `info`, `warn`, `error`                                                          |
| `RATE_LIMIT_MAX`      | `10`     | Maximum requests per window per client                                                    |
| `RATE_LIMIT_WINDOW`   | `1h`     | Rate-limit window                                                                         |
| `TRUST_FORWARDED_FOR` | `false`  | Read `X-Forwarded-For` when extracting the client IP (only enable behind a trusted proxy) |
| `READ_HEADER_TIMEOUT` | `5s`     | HTTP server `ReadHeaderTimeout`                                                           |
| `WRITE_TIMEOUT`       | `10s`    | HTTP server `WriteTimeout`                                                                |
| `IDLE_TIMEOUT`        | `60s`    | HTTP server `IdleTimeout`                                                                 |
| `SHUTDOWN_TIMEOUT`    | `10s`    | Graceful shutdown grace period                                                            |

## Building and running

```sh
# Tests
go test -race ./...

# Local binary
go build -o superphenix-telemetry ./cmd/server

# Run
./superphenix-telemetry
```

Or via Docker:

```sh
docker build -t superphenix-telemetry .
docker run --rm -p 8080:8080 superphenix-telemetry
```

## Deploying with Helm

A Helm chart is in `charts/superphenix-telemetry/`. Tagged releases push
both the container image and the chart to GHCR via the workflow in
`.github/workflows/release-github.yaml`.

```sh
helm install superphenix-telemetry oci://ghcr.io/super-phenix/helm-charts/superphenix-telemetry \
  --namespace superphenix-telemetry --create-namespace
```

The chart values are documented in `charts/superphenix-telemetry/README.md`,
which is generated by [helm-docs](https://github.com/norwoodj/helm-docs)
from the inline comments in `values.yaml`.

```sh
helm-docs --chart-search-root charts
```

## Operational notes

- The chart defaults to `replicaCount: 1`. The rate limiter and the
  IP-hash salt are in-process; running multiple replicas means quotas
  are tracked independently per pod and a client could exceed the
  advertised total. Either keep a single replica, place the rate limit
  at the ingress, or front the service with a sticky-session proxy.
- The server is stateless beyond the in-memory limiter. Restarts wipe
  the limiter state, but the IP-hash salt is static to ensure
  consistent identification across instances.
- `/metrics` exposes the server's own runtime metrics (process_*,
  go_*) alongside ingested data. Restrict this endpoint at the network
  layer if you do not want it public.

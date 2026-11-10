# ocli — Observability CLI

Minimalist, terminal-first CLI in Go for querying Grafana panels, dashboards, alerts, and PromQL engines (Prometheus, Thanos, VictoriaMetrics) with zero bloat and muscle-memory ergonomics.

## Installation

Install globally into `$GOPATH/bin` so `ocli` is available from anywhere in your terminal:

```bash
go install .
```

## Configuration

Run interactive setup or configure via environment variables (`OCLI_URL`, `OCLI_TOKEN`):

```bash
ocli setup
```

Configuration is saved in `~/.ocli.yaml`, including DataSource aliases for instant single-letter routing:

```yaml
url: https://grafana.example.com
token: glsa_xxxxxxxxxxxxxxxxxxxx
datasources:
  default: Prometheus-Prod
  t: Thanos-Prod-DS
  v: Victoria-Metrics-DS
```

## Command Reference

| Command | Aliases / Shortcuts | Arguments / Flags | Description |
| :--- | :--- | :--- | :--- |
| `ocli setup` | `ocli s` | — | Configure Grafana URL, API Token, and DataSource aliases in `~/.ocli.yaml` |
| `ocli dashboard ls` | `dash ls`, `d ls`, `d` | — | List all Grafana dashboards (`UID`, `TITLE`, `FOLDER`, `TAGS`) |
| `ocli pnl <DASH_UID> <PANEL_ID>` | `panel`, `p` | Positional: `<DASH_UID> <PANEL_ID>` | Fetch live metric value from a panel (with threshold ANSI color status) |
| `ocli pnl ls <DASH_UID>` | `pnl list`, `pnl l` | Positional: `<DASH_UID>` | List all panels in a dashboard (`ID`, `TYPE`, `TITLE`, `DESCRIPTION`) |
| `ocli pnl info <DASH_UID> <PANEL_ID>` | `pnl i`, `pnl inspect` | Positional: `<DASH_UID> <PANEL_ID>` | Inspect panel configuration, DataSource, PromQL queries, and transforms |
| `ocli pql "<QUERY>"` | `promql`, `q` | `-t` (Thanos), `-v` (VM), `-d`/`--ds <alias>` | Execute an instant PromQL query against default or selected DataSource |
| `ocli alerts` | `ocli a` | — | List all currently `Firing` alerts in Grafana |

## Examples & Output

### 1. List All Dashboards
```bash
ocli dash ls
# or: ocli d ls / ocli dashboard ls
```
```text
UID           | TITLE                 | FOLDER      | TAGS
--------------+-----------------------+-------------+-------------------
app-deep-dive | App Deep Dive         | Production  | [otel, tracing]
shoham-infra  | Shoham Observability  | Infra       | [network, eBPF]
syn-exp       | Synthetic Exporter    | Testing     | [playwright]
```

### 2. Fetch Panel Value
```bash
ocli pnl 123 4
```
```text
[ CPU Usage ] 42.5%
```

### 3. List Dashboard Panels
```bash
ocli pnl ls 123
```
```text
ID  | TYPE       | TITLE                 | DESCRIPTION
----+------------+-----------------------+--------------------------------
2   | timeseries | CPU Usage             | Node CPU utilization
4   | stat       | Active Users          | Current connected users
12  | log        | Error Logs            | Filtered by level=error
```

### 4. Inspect Panel Configuration
```bash
ocli pnl info 123 4
```
```text
[ Panel Info: Active Users (ID: 4) ]
Dashboard:  123
Type:       stat
DataSource: Prometheus-Prod
Query A:    sum(rate(http_requests_total{status="200"}[5m]))
Transform:  Organize fields (Hidden: Time)
```

### 5. Execute PromQL Queries (Prometheus / Thanos / VictoriaMetrics)
```bash
# Default Prometheus DataSource
ocli pql "sum(servers) - 4"

# Thanos DataSource (mapped to datasources.t in ~/.ocli.yaml)
ocli pql -t "up == 0"

# VictoriaMetrics DataSource (mapped to datasources.v in ~/.ocli.yaml)
ocli pql -v "rate(http_requests_total[5m])"

# Custom DataSource alias or name
ocli pql -d dev "node_load1"
```
```text
VALUE      | LABELS
-----------------------------
12         | {}
```

### 6. Firing Alerts
```bash
ocli a
```
```text
[FIRING] HighMemoryUsage - Host memory above 90%
```

## Project Structure

```text
.
├── main.go
├── cmd/
│   ├── root.go      # Root command & Viper config loader (~/.ocli.yaml / ENV)
│   ├── setup.go     # Interactive/ENV setup command
│   ├── dashboard.go # Dashboard commands: dashboard ls, dash ls, d ls
│   ├── panel.go     # Panel commands: pnl, pnl ls, pnl info
│   ├── pql.go       # PromQL execution & DataSource alias routing (-t, -v, -d)
│   └── alerts.go    # Firing alerts command
└── internal/
    └── grafana/
        └── client.go # Zero-dependency net/http Grafana API client
```


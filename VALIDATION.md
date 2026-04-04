# Validation

## Automated Baseline

Run from the module root:

```bash
go test ./...
go vet ./...
```

## Metrics Boundary Verified In Current Runtime

- `mysql.GetMetricsGatherer()` exposes a private Prometheus registry, but the plugin does not publish an HTTP endpoint by itself.
- The current protobuf schema only wires DSN and pool fields from `lynx.mysql`; it does not expose a `lynx.mysql.prometheus` block or slow-query toggles such as `slow_query_enabled` / `slow_query_threshold`.
- As a result, only pool, health-check, and connect lifecycle metrics are emitted automatically by the stock plugin runtime.
- The following series require caller-side `lynx-sql-sdk` helper or recorder integration and must not be treated as out-of-the-box metrics:
  `lynx_mysql_query_duration_seconds`,
  `lynx_mysql_errors_total`,
  `lynx_mysql_slow_queries_total`,
  `lynx_mysql_tx_duration_seconds`
- Raw `*sql.DB` and `*sql.Tx` calls (`QueryContext`, `QueryRowContext`, `ExecContext`, `BeginTx`, `Commit`, `Rollback`) do not increment those helper-dependent metrics on their own.

## Required Caller-Side Integration

To emit query, error, and slow-query metrics, application code must build a `lynx-sql-sdk/base.QueryMonitor` with the concrete MySQL plugin's recorder and execute SQL through `MonitorQuery`, `MonitorQueryRow`, or `MonitorExec`.

```go
plugin := lynx.Lynx().GetPluginManager().GetPlugin("mysql.client").(*mysql.DBMysqlClient)
monitor := base.NewQueryMonitor(true, time.Second, plugin.GetMetricsRecorder())

_, err := monitor.MonitorExec(ctx, db, "UPDATE users SET name = ? WHERE id = ?", []interface{}{name, id})
```

To emit transaction duration metrics, application code must time the transaction and call the recorder explicitly:

```go
start := time.Now()
tx, err := db.BeginTx(ctx, nil)
// application-owned transaction body
recorder := plugin.GetMetricsRecorder()
recorder.RecordTx(time.Since(start), err == nil)
```

## Recommended Manual Check

- Merge `mysql.GetMetricsGatherer()` into your application's `/metrics` endpoint.
- Execute one query through raw `db.ExecContext` and confirm pool/health/connect metrics exist while query/transaction series remain unchanged.
- Execute the same query through `base.NewQueryMonitor(...).MonitorExec(...)` and confirm `lynx_mysql_query_duration_seconds`, `lynx_mysql_errors_total`, or `lynx_mysql_slow_queries_total` begin to move as expected.
- Wrap one transaction in application timing logic and confirm `lynx_mysql_tx_duration_seconds` changes only after an explicit `RecordTx(...)` call.

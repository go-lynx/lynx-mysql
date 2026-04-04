package mysql

import (
	"time"

	"github.com/go-lynx/lynx-mysql/conf"
	"github.com/go-lynx/lynx-sql-sdk/base"
)

// mysqlMetricsAdapter bridges the sql-sdk MetricsRecorder interface to the plugin's private Prometheus registry.
type mysqlMetricsAdapter struct {
	pm     *PrometheusMetrics
	config *conf.Mysql
}

func newMysqlMetricsAdapter(pm *PrometheusMetrics, config *conf.Mysql) *mysqlMetricsAdapter {
	return &mysqlMetricsAdapter{pm: pm, config: config}
}

func (a *mysqlMetricsAdapter) RecordConnectionPoolStats(stats *base.ConnectionPoolStats) {
	if a.pm != nil {
		a.pm.UpdateMetrics(stats, a.config)
	}
}

func (a *mysqlMetricsAdapter) RecordHealthCheck(success bool) {
	if a.pm != nil {
		a.pm.RecordHealthCheck(success, a.config)
	}
}

func (a *mysqlMetricsAdapter) RecordQuery(duration time.Duration, err error, threshold time.Duration) {
	if a.pm != nil {
		a.pm.RecordQuery("query", duration, err, threshold, a.config, "")
	}
}

func (a *mysqlMetricsAdapter) RecordTx(duration time.Duration, committed bool) {
	if a.pm != nil {
		a.pm.RecordTx(duration, committed, a.config)
	}
}

func (a *mysqlMetricsAdapter) IncConnectAttempt() {
	if a.pm != nil {
		a.pm.IncConnectAttempt(a.config)
	}
}

func (a *mysqlMetricsAdapter) IncConnectRetry() {
	if a.pm != nil {
		a.pm.IncConnectRetry(a.config)
	}
}

func (a *mysqlMetricsAdapter) IncConnectSuccess() {
	if a.pm != nil {
		a.pm.IncConnectSuccess(a.config)
	}
}

func (a *mysqlMetricsAdapter) IncConnectFailure() {
	if a.pm != nil {
		a.pm.IncConnectFailure(a.config)
	}
}

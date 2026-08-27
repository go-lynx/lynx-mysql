// Package mysql provides a MySQL client plugin for the Lynx framework.
// It wraps database/sql with a production-grade connection pool, Prometheus metrics
// (connection pool stats, query/transaction latency, slow-query counters), configurable
// health checks, and lifecycle management driven by protobuf-based configuration.
package mysql

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-lynx/lynx-mysql/conf"
	"github.com/go-lynx/lynx-sql-sdk/base"
	"github.com/go-lynx/lynx-sql-sdk/interfaces"
	"github.com/go-lynx/lynx/log"
	"github.com/go-lynx/lynx/plugins"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"

	// MySQL driver
	_ "github.com/go-sql-driver/mysql"
)

// Plugin metadata
const (
	pluginName        = "mysql.client"
	pluginVersion     = "v1.6.3"
	pluginDescription = "mysql client plugin for lynx framework"
	confPrefix        = "lynx.mysql"
)

// DBMysqlClient represents MySQL client plugin instance
type DBMysqlClient struct {
	*base.SQLPlugin
	config            *interfaces.Config
	pbConfig          *conf.Mysql // protobuf configuration
	prometheusMetrics *PrometheusMetrics
	lifecycleMu       sync.Mutex
	mu                sync.RWMutex
}

func defaultConfig() *interfaces.Config {
	return &interfaces.Config{
		Driver: "mysql",
		// Default connection pool settings
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 3600, // 1 hour
		ConnMaxIdleTime: 300,  // 5 minutes
		// Default health check settings
		HealthCheckInterval: 30, // 30 seconds
		HealthCheckQuery:    "SELECT 1",
	}
}

func newSQLPlugin(config *interfaces.Config) *base.SQLPlugin {
	p := base.NewBaseSQLPlugin(
		plugins.GeneratePluginID("", pluginName, pluginVersion),
		pluginName,
		pluginDescription,
		pluginVersion,
		confPrefix,
		101,
		config,
	)
	// Register the stable provider so the sdk publishes the shared/private provider resources.
	p.SetProvider(dbProvider{})
	return p
}

// NewMysqlClient creates a new MySQL client plugin instance
func NewMysqlClient() *DBMysqlClient {
	config := defaultConfig()

	c := &DBMysqlClient{
		config:   config,
		pbConfig: &conf.Mysql{},
	}

	c.SQLPlugin = newSQLPlugin(config)

	return c
}

// InitializeResources loads protobuf configuration and initializes resources.
// Config is loaded from the proto schema; base plugin validation is applied via
// InitializeFromConfig to avoid a second scan of the same YAML key.
func (m *DBMysqlClient) InitializeResources(rt plugins.Runtime) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	m.mu.RLock()
	currentSQLPlugin := m.SQLPlugin
	m.mu.RUnlock()
	if currentSQLPlugin != nil && currentSQLPlugin.IsConnected() {
		return fmt.Errorf("cannot reinitialize mysql plugin while database connection is active")
	}

	pbConfig := &conf.Mysql{}
	if err := rt.GetConfig().Value(confPrefix).Scan(pbConfig); err != nil {
		return fmt.Errorf("failed to load MySQL configuration: %w", err)
	}

	config := defaultConfig()

	if pbConfig.Driver != "" {
		config.Driver = pbConfig.Driver
	}
	if pbConfig.Source != "" {
		config.DSN = pbConfig.Source
	}
	if pbConfig.MaxConn > 0 {
		config.MaxOpenConns = int(pbConfig.MaxConn)
	}
	if pbConfig.MinConn > 0 {
		config.MaxIdleConns = int(pbConfig.MinConn)
		config.WarmupEnabled = true
		config.WarmupConns = int(pbConfig.MinConn)
	}
	if pbConfig.MaxIdleConn > 0 {
		config.MaxIdleConns = int(pbConfig.MaxIdleConn)
		if pbConfig.MinConn == 0 {
			config.WarmupEnabled = true
			config.WarmupConns = int(pbConfig.MaxIdleConn)
		}
	}
	if pbConfig.MaxLifeTime != nil {
		config.ConnMaxLifetime = int(pbConfig.MaxLifeTime.AsDuration().Seconds())
	}
	if pbConfig.MaxIdleTime != nil {
		config.ConnMaxIdleTime = int(pbConfig.MaxIdleTime.AsDuration().Seconds())
	}
	if config.MaxIdleConns > config.MaxOpenConns {
		config.MaxIdleConns = config.MaxOpenConns
	}
	if config.WarmupConns > config.MaxOpenConns {
		config.WarmupConns = config.MaxOpenConns
	}

	// Reuse the SQLPlugin built by NewMysqlClient (its config is refreshed in place
	// from `config` below); only rebuild it after it has been closed.
	sqlPlugin := currentSQLPlugin
	rebuilt := false
	if sqlPlugin == nil || sqlPlugin.IsClosed() {
		sqlPlugin = newSQLPlugin(config)
		rebuilt = true
	} else {
		// The reused plugin owns m.config (see NewMysqlClient); refresh it in place
		// so the plugin sees the freshly loaded settings.
		m.mu.RLock()
		existing := m.config
		m.mu.RUnlock()
		*existing = *config
	}

	// InitializeFromConfig applies defaults+validation without re-scanning the YAML key,
	// preventing proto values from being silently overwritten by a second scan.
	if err := sqlPlugin.InitializeFromConfig(rt); err != nil {
		return err
	}

	metrics := NewPrometheusMetrics(createPrometheusConfig(pbConfig))
	sqlPlugin.SetMetricsRecorder(newMysqlMetricsAdapter(metrics, cloneMysqlConfig(pbConfig)))

	m.mu.Lock()
	m.pbConfig = pbConfig
	if rebuilt {
		m.config = config
	}
	// When the plugin is reused, m.config is the plugin's own config struct and has just
	// been refreshed in place from `config` above.
	m.prometheusMetrics = metrics
	m.SQLPlugin = sqlPlugin
	m.mu.Unlock()

	return nil
}

// StartupTasks initializes database connection (legacy, non-cancellable
// entrypoint; delegates to StartupTasksContext).
func (m *DBMysqlClient) StartupTasks() error {
	return m.StartupTasksContext(context.Background())
}

// StartupTasksContext initializes the database connection while honoring ctx.
// The connect/ping/retry work in the embedded SQLPlugin is bound to ctx, and the
// metrics updater is only started once the connection was established.
func (m *DBMysqlClient) StartupTasksContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("mysql startup canceled before execution: %w", err)
	}

	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	log.Infof("initializing mysql database connection")

	m.mu.RLock()
	sqlPlugin := m.SQLPlugin
	config := m.config
	m.mu.RUnlock()

	if sqlPlugin == nil {
		return fmt.Errorf("mysql SQL plugin is not initialized")
	}

	if err := sqlPlugin.StartupTasksContext(ctx); err != nil {
		return err
	}

	log.Infof("mysql database successfully initialized with connection pool: max_open=%d, max_idle=%d",
		config.MaxOpenConns, config.MaxIdleConns)
	return nil
}

// CleanupTasks gracefully closes database connection
func (m *DBMysqlClient) CleanupTasks() error {
	return m.CleanupTasksContext(context.Background())
}

// CleanupTasksContext gracefully closes the database connection while honoring ctx.
func (m *DBMysqlClient) CleanupTasksContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("mysql cleanup canceled before execution: %w", err)
	}

	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	log.Infof("closing mysql database connection")
	m.mu.RLock()
	sqlPlugin := m.SQLPlugin
	m.mu.RUnlock()
	if sqlPlugin == nil {
		return nil
	}
	err := sqlPlugin.CleanupTasksContext(ctx)
	if errors.Is(err, base.ErrAlreadyClosed) {
		return nil
	}
	return err
}

// GetMetricsGatherer returns the Prometheus Gatherer for this plugin's metrics, or nil if metrics are unavailable.
func (m *DBMysqlClient) GetMetricsGatherer() prometheus.Gatherer {
	m.mu.RLock()
	metrics := m.prometheusMetrics
	m.mu.RUnlock()
	if metrics == nil {
		return nil
	}
	return metrics.GetGatherer()
}

func cloneMysqlConfig(cfg *conf.Mysql) *conf.Mysql {
	if cfg == nil {
		return nil
	}
	return proto.Clone(cfg).(*conf.Mysql)
}

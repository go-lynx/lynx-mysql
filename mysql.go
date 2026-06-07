// Package mysql provides a MySQL client plugin for the Lynx framework.
// It wraps database/sql with a production-grade connection pool, Prometheus metrics
// (connection pool stats, query/transaction latency, slow-query counters), configurable
// health checks, and lifecycle management driven by protobuf-based configuration.
package mysql

import (
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
	return base.NewBaseSQLPlugin(
		plugins.GeneratePluginID("", pluginName, pluginVersion),
		pluginName,
		pluginDescription,
		pluginVersion,
		confPrefix,
		101,
		config,
	)
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

	sqlPlugin := newSQLPlugin(config)
	// InitializeFromConfig applies defaults+validation without re-scanning the YAML key,
	// preventing proto values from being silently overwritten by a second scan.
	if err := sqlPlugin.InitializeFromConfig(rt); err != nil {
		return err
	}

	metrics := NewPrometheusMetrics(createPrometheusConfig(pbConfig))
	sqlPlugin.SetMetricsRecorder(newMysqlMetricsAdapter(metrics, cloneMysqlConfig(pbConfig)))

	m.mu.Lock()
	m.pbConfig = pbConfig
	m.config = config
	m.prometheusMetrics = metrics
	m.SQLPlugin = sqlPlugin
	m.mu.Unlock()

	return nil
}

// StartupTasks initializes database connection
func (m *DBMysqlClient) StartupTasks() error {
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

	if err := sqlPlugin.StartupTasks(); err != nil {
		return err
	}

	log.Infof("mysql database successfully initialized with connection pool: max_open=%d, max_idle=%d",
		config.MaxOpenConns, config.MaxIdleConns)
	return nil
}

// CleanupTasks gracefully closes database connection
func (m *DBMysqlClient) CleanupTasks() error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	log.Infof("closing mysql database connection")
	m.mu.RLock()
	sqlPlugin := m.SQLPlugin
	m.mu.RUnlock()
	if sqlPlugin == nil {
		return nil
	}
	err := sqlPlugin.CleanupTasks()
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

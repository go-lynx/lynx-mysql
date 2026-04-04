package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	kratosconfig "github.com/go-kratos/kratos/v2/config"
	"github.com/go-lynx/lynx-mysql/conf"
	"github.com/go-lynx/lynx-sql-sdk/base"
	"github.com/go-lynx/lynx-sql-sdk/interfaces"
	"github.com/go-lynx/lynx/log"
	"github.com/go-lynx/lynx/plugins"
	"github.com/prometheus/client_golang/prometheus"

	// MySQL driver
	_ "github.com/go-sql-driver/mysql"
)

// Plugin metadata
const (
	pluginName        = "mysql.client"
	pluginVersion     = "v1.5.4"
	pluginDescription = "mysql client plugin for lynx framework"
	confPrefix        = "lynx.mysql"
)

// DBMysqlClient represents MySQL client plugin instance
type DBMysqlClient struct {
	*base.SQLPlugin
	config            *interfaces.Config
	pbConfig          *conf.Mysql // protobuf configuration
	prometheusMetrics *PrometheusMetrics
	metricsCancel     context.CancelFunc
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

// InitializeResources loads protobuf configuration and initializes resources
func (m *DBMysqlClient) InitializeResources(rt plugins.Runtime) error {
	if m.metricsCancel != nil {
		m.metricsCancel()
		m.metricsCancel = nil
	}

	// Load protobuf configuration to a temporary variable first
	// This ensures we don't partially update m.pbConfig if loading fails
	pbConfig := &conf.Mysql{}
	if err := rt.GetConfig().Value(confPrefix).Scan(pbConfig); err != nil {
		return fmt.Errorf("failed to load MySQL configuration: %w", err)
	}

	config := defaultConfig()

	// Preserve the runtime default driver when config omits it.
	if pbConfig.Driver != "" {
		config.Driver = pbConfig.Driver
	}

	// Support both 'source' (protobuf field) and 'dsn' (common alias)
	// Configuration system may map 'dsn' to 'source' automatically
	if pbConfig.Source != "" {
		config.DSN = pbConfig.Source
	}

	// Map max_conn to MaxOpenConns (maximum open connections)
	if pbConfig.MaxConn > 0 {
		config.MaxOpenConns = int(pbConfig.MaxConn)
	}

	// Map min_conn to MaxIdleConns (maximum idle connections)
	// Also enable warmup to pre-create connections if min_conn is set
	if pbConfig.MinConn > 0 {
		config.MaxIdleConns = int(pbConfig.MinConn)
		// Enable connection pool warmup to pre-create connections up to min_conn
		config.WarmupEnabled = true
		config.WarmupConns = int(pbConfig.MinConn)
	}

	// Handle max_idle_conn if explicitly set (takes precedence over min_conn)
	if pbConfig.MaxIdleConn > 0 {
		config.MaxIdleConns = int(pbConfig.MaxIdleConn)
		// Update warmup count if not already set by min_conn
		if pbConfig.MinConn == 0 {
			config.WarmupEnabled = true
			config.WarmupConns = int(pbConfig.MaxIdleConn)
		}
	}

	// Handle duration fields
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

	m.pbConfig = pbConfig
	m.config = config
	m.prometheusMetrics = nil
	m.metricsCancel = nil
	m.SQLPlugin = newSQLPlugin(config)

	if err := m.SQLPlugin.InitializeResources(&runtimeConfigAdapter{
		Runtime: rt,
		config:  config,
	}); err != nil {
		return err
	}

	m.prometheusMetrics = NewPrometheusMetrics(createPrometheusConfig(pbConfig))
	m.SQLPlugin.SetMetricsRecorder(newMysqlMetricsAdapter(m.prometheusMetrics, m.pbConfig))

	return nil
}

// StartupTasks initializes database connection
func (m *DBMysqlClient) StartupTasks() error {
	log.Infof("initializing mysql database connection")

	if err := m.SQLPlugin.StartupTasks(); err != nil {
		return err
	}

	if m.prometheusMetrics != nil {
		if m.metricsCancel != nil {
			m.metricsCancel()
		}
		m.prometheusMetrics.UpdateMetrics(m.SQLPlugin.GetStats(), m.pbConfig)

		ctx, cancel := context.WithCancel(context.Background())
		m.metricsCancel = cancel
		go m.runPoolStatsUpdater(ctx)
	}

	log.Infof("mysql database successfully initialized with connection pool: max_open=%d, max_idle=%d",
		m.config.MaxOpenConns, m.config.MaxIdleConns)
	return nil
}

func (m *DBMysqlClient) runPoolStatsUpdater(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.prometheusMetrics != nil && m.SQLPlugin != nil {
				m.prometheusMetrics.UpdateMetrics(m.SQLPlugin.GetStats(), m.pbConfig)
			}
		}
	}
}

// CleanupTasks gracefully closes database connection
func (m *DBMysqlClient) CleanupTasks() error {
	if m.metricsCancel != nil {
		m.metricsCancel()
		m.metricsCancel = nil
	}

	log.Infof("closing mysql database connection")
	err := m.SQLPlugin.CleanupTasks()
	if errors.Is(err, base.ErrAlreadyClosed) {
		return nil
	}
	return err
}

// GetMetricsGatherer returns the Prometheus Gatherer for this plugin's metrics, or nil if metrics are unavailable.
func (m *DBMysqlClient) GetMetricsGatherer() prometheus.Gatherer {
	if m.prometheusMetrics == nil {
		return nil
	}
	return m.prometheusMetrics.GetGatherer()
}

type runtimeConfigAdapter struct {
	plugins.Runtime
	config *interfaces.Config
}

func (r *runtimeConfigAdapter) GetConfig() kratosconfig.Config {
	return &configAdapter{config: r.config}
}

type configAdapter struct {
	config *interfaces.Config
}

func (c *configAdapter) Value(key string) kratosconfig.Value {
	return &configValueAdapter{config: c.config}
}

func (c *configAdapter) Scan(dest any) error                             { return nil }
func (c *configAdapter) Load() error                                     { return nil }
func (c *configAdapter) Watch(key string, o kratosconfig.Observer) error { return nil }
func (c *configAdapter) Close() error                                    { return nil }

type configValueAdapter struct {
	config *interfaces.Config
}

func (v *configValueAdapter) Scan(dest any) error {
	cfg, ok := dest.(*interfaces.Config)
	if !ok {
		return nil
	}
	*cfg = *v.config
	return nil
}

func (v *configValueAdapter) Bool() (bool, error)                  { return false, nil }
func (v *configValueAdapter) Int() (int64, error)                  { return 0, nil }
func (v *configValueAdapter) Float() (float64, error)              { return 0, nil }
func (v *configValueAdapter) String() (string, error)              { return "", nil }
func (v *configValueAdapter) Duration() (time.Duration, error)     { return 0, nil }
func (v *configValueAdapter) Slice() ([]kratosconfig.Value, error) { return nil, nil }
func (v *configValueAdapter) Map() (map[string]kratosconfig.Value, error) {
	return nil, nil
}
func (v *configValueAdapter) Load() any     { return v.config }
func (v *configValueAdapter) Store(val any) {}

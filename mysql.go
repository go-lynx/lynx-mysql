package mysql

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	kratosconfig "github.com/go-kratos/kratos/v2/config"
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
	metricsWG         sync.WaitGroup
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

// InitializeResources loads protobuf configuration and initializes resources
func (m *DBMysqlClient) InitializeResources(rt plugins.Runtime) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	m.stopMetricsUpdater()
	m.mu.RLock()
	currentSQLPlugin := m.SQLPlugin
	m.mu.RUnlock()
	if currentSQLPlugin != nil && currentSQLPlugin.IsConnected() {
		return fmt.Errorf("cannot reinitialize mysql plugin while database connection is active")
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

	sqlPlugin := newSQLPlugin(config)

	if err := sqlPlugin.InitializeResources(&runtimeConfigAdapter{
		Runtime: rt,
		config:  config,
	}); err != nil {
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
	metrics := m.prometheusMetrics
	pbConfig := cloneMysqlConfig(m.pbConfig)
	config := m.config
	m.mu.RUnlock()

	if sqlPlugin == nil {
		return fmt.Errorf("mysql SQL plugin is not initialized")
	}

	if err := sqlPlugin.StartupTasks(); err != nil {
		return err
	}

	if metrics != nil {
		m.stopMetricsUpdater()
		metrics.UpdateMetrics(sqlPlugin.GetStats(), pbConfig)

		ctx, cancel := context.WithCancel(context.Background())
		m.mu.Lock()
		m.metricsCancel = cancel
		m.mu.Unlock()
		m.metricsWG.Add(1)
		go m.runPoolStatsUpdater(ctx, sqlPlugin, metrics, pbConfig)
	}

	log.Infof("mysql database successfully initialized with connection pool: max_open=%d, max_idle=%d",
		config.MaxOpenConns, config.MaxIdleConns)
	return nil
}

func (m *DBMysqlClient) runPoolStatsUpdater(ctx context.Context, sqlPlugin *base.SQLPlugin, metrics *PrometheusMetrics, pbConfig *conf.Mysql) {
	defer m.metricsWG.Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if metrics != nil && sqlPlugin != nil {
				metrics.UpdateMetrics(sqlPlugin.GetStats(), pbConfig)
			}
		}
	}
}

// CleanupTasks gracefully closes database connection
func (m *DBMysqlClient) CleanupTasks() error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	m.stopMetricsUpdater()

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

func (m *DBMysqlClient) stopMetricsUpdater() {
	m.mu.Lock()
	cancel := m.metricsCancel
	m.metricsCancel = nil
	m.mu.Unlock()

	if cancel != nil {
		cancel()
		m.metricsWG.Wait()
	}
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

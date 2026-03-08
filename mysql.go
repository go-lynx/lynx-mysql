package mysql

import (
	"fmt"

	"github.com/go-lynx/lynx-mysql/conf"
	"github.com/go-lynx/lynx-sql-sdk/base"
	"github.com/go-lynx/lynx-sql-sdk/interfaces"
	"github.com/go-lynx/lynx/log"
	"github.com/go-lynx/lynx/plugins"

	// MySQL driver
	_ "github.com/go-sql-driver/mysql"
)

// Plugin metadata
const (
	pluginName        = "mysql.client"
	pluginVersion     = "v1.5.5"
	pluginDescription = "mysql client plugin for lynx framework"
	confPrefix        = "lynx.mysql"
)

// DBMysqlClient represents MySQL client plugin instance
type DBMysqlClient struct {
	*base.SQLPlugin
	config   *interfaces.Config
	pbConfig *conf.Mysql // protobuf configuration
}

// NewMysqlClient creates a new MySQL client plugin instance
func NewMysqlClient() *DBMysqlClient {
	config := &interfaces.Config{
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

	c := &DBMysqlClient{
		config:   config,
		pbConfig: &conf.Mysql{},
	}

	c.SQLPlugin = base.NewBaseSQLPlugin(
		plugins.GeneratePluginID("", pluginName, pluginVersion),
		pluginName,
		pluginDescription,
		pluginVersion,
		confPrefix,
		101,
		config,
	)

	return c
}

// InitializeResources loads protobuf configuration and initializes resources
func (m *DBMysqlClient) InitializeResources(rt plugins.Runtime) error {
	// Load protobuf configuration to a temporary variable first
	// This ensures we don't partially update m.pbConfig if loading fails
	pbConfig := &conf.Mysql{}
	if err := rt.GetConfig().Value(confPrefix).Scan(pbConfig); err != nil {
		return fmt.Errorf("failed to load MySQL configuration: %w", err)
	}

	// Only update m.pbConfig after successful loading
	m.pbConfig = pbConfig

	// Update interfaces.Config from protobuf config
	// This ensures atomic update - either all fields are updated or none
	m.config.Driver = pbConfig.Driver

	// Support both 'source' (protobuf field) and 'dsn' (common alias)
	// Configuration system may map 'dsn' to 'source' automatically
	if pbConfig.Source != "" {
		m.config.DSN = pbConfig.Source
	}

	// Map max_conn to MaxOpenConns (maximum open connections)
	if pbConfig.MaxConn > 0 {
		m.config.MaxOpenConns = int(pbConfig.MaxConn)
	}

	// Map min_conn to MaxIdleConns (maximum idle connections)
	// Also enable warmup to pre-create connections if min_conn is set
	if pbConfig.MinConn > 0 {
		m.config.MaxIdleConns = int(pbConfig.MinConn)
		// Enable connection pool warmup to pre-create connections up to min_conn
		m.config.WarmupEnabled = true
		m.config.WarmupConns = int(pbConfig.MinConn)
	}

	// Handle max_idle_conn if explicitly set (takes precedence over min_conn)
	if pbConfig.MaxIdleConn > 0 {
		m.config.MaxIdleConns = int(pbConfig.MaxIdleConn)
		// Update warmup count if not already set by min_conn
		if pbConfig.MinConn == 0 {
			m.config.WarmupEnabled = true
			m.config.WarmupConns = int(pbConfig.MaxIdleConn)
		}
	}

	// Handle duration fields
	if pbConfig.MaxLifeTime != nil {
		m.config.ConnMaxLifetime = int(pbConfig.MaxLifeTime.AsDuration().Seconds())
	}
	if pbConfig.MaxIdleTime != nil {
		m.config.ConnMaxIdleTime = int(pbConfig.MaxIdleTime.AsDuration().Seconds())
	}

	// Call parent initialization to set defaults and validate
	return m.SQLPlugin.InitializeResources(rt)
}

// StartupTasks initializes database connection
func (m *DBMysqlClient) StartupTasks() error {
	log.Infof("initializing mysql database connection")

	if err := m.SQLPlugin.StartupTasks(); err != nil {
		return err
	}

	log.Infof("mysql database successfully initialized with connection pool: max_open=%d, max_idle=%d",
		m.config.MaxOpenConns, m.config.MaxIdleConns)
	return nil
}

// CleanupTasks gracefully closes database connection
func (m *DBMysqlClient) CleanupTasks() error {
	log.Infof("closing mysql database connection")
	return m.SQLPlugin.CleanupTasks()
}

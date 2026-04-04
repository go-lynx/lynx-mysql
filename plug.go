package mysql

import (
	"context"
	"database/sql"
	"fmt"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-lynx/lynx"
	"github.com/go-lynx/lynx-sql-sdk/interfaces"
	"github.com/go-lynx/lynx/pkg/factory"
	"github.com/go-lynx/lynx/plugins"
	"github.com/prometheus/client_golang/prometheus"
)

// DBProvider resolves the current pool on each call so callers do not cache a stale *sql.DB across reconnects.
type DBProvider interface {
	DB(ctx context.Context) (*sql.DB, error)
	ValidatedConn(ctx context.Context) (*sql.Conn, error)
	Dialect() string
}

type dbProvider struct{}

// init function registers the MySQL plugin to the global plugin factory.
// This function is automatically called when the package is imported.
func init() {
	factory.GlobalTypedFactory().RegisterPlugin(pluginName, confPrefix, func() plugins.Plugin {
		return NewMysqlClient()
	})
}

// GetDB gets the database connection from the MySQL plugin
func GetDB() (*sql.DB, error) {
	if lynx.Lynx() == nil {
		return nil, fmt.Errorf("lynx not initialized")
	}
	plugin := lynx.Lynx().GetPluginManager().GetPlugin(pluginName)
	if plugin == nil {
		return nil, fmt.Errorf("plugin %s not found", pluginName)
	}
	if sqlPlugin, ok := plugin.(interfaces.SQLPlugin); ok {
		return sqlPlugin.GetDB()
	}
	return nil, fmt.Errorf("plugin %s is not a SQLPlugin", pluginName)
}

// GetDBWithContext gets the database connection from the MySQL plugin with context support.
func GetDBWithContext(ctx context.Context) (*sql.DB, error) {
	if lynx.Lynx() == nil {
		return nil, fmt.Errorf("lynx not initialized")
	}
	plugin := lynx.Lynx().GetPluginManager().GetPlugin(pluginName)
	if plugin == nil {
		return nil, fmt.Errorf("plugin %s not found", pluginName)
	}
	if sqlPlugin, ok := plugin.(interfaces.SQLPlugin); ok {
		return sqlPlugin.GetDBWithContext(ctx)
	}
	return nil, fmt.Errorf("plugin %s is not a SQLPlugin", pluginName)
}

// GetValidatedConn returns a validated connection from the current MySQL pool.
func GetValidatedConn(ctx context.Context) (*sql.Conn, error) {
	db, err := GetDBWithContext(ctx)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// GetProvider returns a stable provider for the current MySQL pool.
// The provider does not hold a concrete *sql.DB; each call resolves the current pool so it remains valid after reconnect.
func GetProvider() DBProvider {
	return dbProvider{}
}

// GetDialect gets the database dialect
func GetDialect() string {
	if lynx.Lynx() == nil {
		return ""
	}
	plugin := lynx.Lynx().GetPluginManager().GetPlugin(pluginName)
	if plugin == nil {
		return ""
	}
	if sqlPlugin, ok := plugin.(interfaces.SQLPlugin); ok {
		return sqlPlugin.GetDialect()
	}
	return ""
}

// IsConnected checks if the database is connected
func IsConnected() bool {
	if lynx.Lynx() == nil {
		return false
	}
	plugin := lynx.Lynx().GetPluginManager().GetPlugin(pluginName)
	if plugin == nil {
		return false
	}
	if sqlPlugin, ok := plugin.(interfaces.SQLPlugin); ok {
		return sqlPlugin.IsConnected()
	}
	return false
}

// CheckHealth performs health check
func CheckHealth() error {
	if lynx.Lynx() == nil {
		return fmt.Errorf("lynx not initialized")
	}
	plugin := lynx.Lynx().GetPluginManager().GetPlugin(pluginName)
	if plugin == nil {
		return fmt.Errorf("plugin %s not found", pluginName)
	}
	if sqlPlugin, ok := plugin.(interfaces.SQLPlugin); ok {
		return sqlPlugin.CheckHealth()
	}
	return fmt.Errorf("plugin %s is not a SQLPlugin", pluginName)
}

func (dbProvider) DB(ctx context.Context) (*sql.DB, error) {
	return GetDBWithContext(ctx)
}

func (dbProvider) ValidatedConn(ctx context.Context) (*sql.Conn, error) {
	return GetValidatedConn(ctx)
}

func (dbProvider) Dialect() string {
	return GetDialect()
}

// GetDriver gets the ent SQL driver from the MySQL plugin.
// Returns an error if the database connection cannot be obtained.
// Do not cache the returned driver when auto-reconnect is enabled; prefer GetDriverProvider for long-lived components.
func GetDriver() (*entsql.Driver, error) {
	db, err := GetDB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}
	return entsql.OpenDB(GetDialect(), db), nil
}

// GetDriverProvider returns a stable provider for ent SQL drivers.
// The returned closure resolves the current pool on each call and should be preferred over caching GetDriver().
func GetDriverProvider() func(ctx context.Context) (*entsql.Driver, error) {
	provider := GetProvider()
	return func(ctx context.Context) (*entsql.Driver, error) {
		db, err := provider.DB(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get database connection: %w", err)
		}
		if db == nil {
			return nil, fmt.Errorf("database connection is nil")
		}
		return entsql.OpenDB(provider.Dialect(), db), nil
	}
}

// GetMetricsGatherer returns the MySQL plugin's private Prometheus gatherer, or nil if the plugin is unavailable.
// Merge this gatherer into the application's /metrics endpoint; the plugin does not expose an HTTP endpoint itself.
func GetMetricsGatherer() prometheus.Gatherer {
	if lynx.Lynx() == nil {
		return nil
	}
	plugin := lynx.Lynx().GetPluginManager().GetPlugin(pluginName)
	if plugin == nil {
		return nil
	}
	if client, ok := plugin.(*DBMysqlClient); ok {
		return client.GetMetricsGatherer()
	}
	return nil
}

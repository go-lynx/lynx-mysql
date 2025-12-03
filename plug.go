package mysql

import (
	"database/sql"
	"fmt"

	"github.com/go-lynx/lynx"
	"github.com/go-lynx/lynx-sql-sdk/interfaces"
	"github.com/go-lynx/lynx/pkg/factory"
	"github.com/go-lynx/lynx/plugins"
)

// init function registers the MySQL plugin to the global plugin factory.
// This function is automatically called when the package is imported.
func init() {
	factory.GlobalTypedFactory().RegisterPlugin(pluginName, confPrefix, func() plugins.Plugin {
		return NewMysqlClient()
	})
}

// GetDB gets the database connection from the MySQL plugin
func GetDB() (*sql.DB, error) {
	plugin := lynx.Lynx().GetPluginManager().GetPlugin(pluginName)
	if plugin == nil {
		return nil, fmt.Errorf("plugin %s not found", pluginName)
	}
	if sqlPlugin, ok := plugin.(interfaces.SQLPlugin); ok {
		return sqlPlugin.GetDB()
	}
	return nil, fmt.Errorf("plugin %s is not a SQLPlugin", pluginName)
}

// GetDialect gets the database dialect
func GetDialect() string {
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
	plugin := lynx.Lynx().GetPluginManager().GetPlugin(pluginName)
	if plugin == nil {
		return fmt.Errorf("plugin %s not found", pluginName)
	}
	if sqlPlugin, ok := plugin.(interfaces.SQLPlugin); ok {
		return sqlPlugin.CheckHealth()
	}
	return fmt.Errorf("plugin %s is not a SQLPlugin", pluginName)
}

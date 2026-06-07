package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-lynx/lynx-mysql/conf"
	"github.com/go-lynx/lynx-sql-sdk/base"
	"github.com/go-lynx/lynx-sql-sdk/interfaces"
	"github.com/go-lynx/lynx/plugins"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

// mockRuntime is a mock implementation of plugins.Runtime for testing
type mockRuntime struct {
	config map[string]any
}

func (m *mockRuntime) GetConfig() config.Config {
	return &mockConfig{values: m.config}
}

func (m *mockRuntime) AddListener(listener plugins.EventListener, filter *plugins.EventFilter) {}
func (m *mockRuntime) AddPluginListener(pluginName string, listener plugins.EventListener, filter *plugins.EventFilter) {
}
func (m *mockRuntime) CleanupResources(pluginName string) error                                 { return nil }
func (m *mockRuntime) EmitEvent(event plugins.PluginEvent)                                      {}
func (m *mockRuntime) EmitPluginEvent(pluginName string, eventType string, data map[string]any) {}
func (m *mockRuntime) GetCurrentPluginContext() string                                          { return "" }
func (m *mockRuntime) GetEventHistory(filter plugins.EventFilter) []plugins.PluginEvent         { return nil }
func (m *mockRuntime) GetEventStats() map[string]any                                            { return nil }
func (m *mockRuntime) GetLogger() log.Logger                                                    { return log.DefaultLogger }
func (m *mockRuntime) GetPluginEventHistory(pluginName string, filter plugins.EventFilter) []plugins.PluginEvent {
	return nil
}
func (m *mockRuntime) GetPrivateResource(name string) (any, error)                { return nil, nil }
func (m *mockRuntime) GetResource(name string) (any, error)                       { return nil, nil }
func (m *mockRuntime) GetResourceInfo(name string) (*plugins.ResourceInfo, error) { return nil, nil }
func (m *mockRuntime) GetResourceStats() map[string]any                           { return nil }
func (m *mockRuntime) GetSharedResource(name string) (any, error)                 { return nil, nil }
func (m *mockRuntime) GetTypedResource(name string, resourceType string) (any, error) {
	return nil, nil
}
func (m *mockRuntime) ListResources() []*plugins.ResourceInfo                  { return nil }
func (m *mockRuntime) RegisterPrivateResource(name string, resource any) error { return nil }
func (m *mockRuntime) RegisterResource(name string, resource any) error        { return nil }
func (m *mockRuntime) RegisterSharedResource(name string, resource any) error  { return nil }
func (m *mockRuntime) RegisterTypedResource(name string, resource any, resourceType string) error {
	return nil
}
func (m *mockRuntime) RemoveListener(listener plugins.EventListener)                          {}
func (m *mockRuntime) RemovePluginListener(pluginName string, listener plugins.EventListener) {}
func (m *mockRuntime) SetConfig(conf config.Config)                                           {}
func (m *mockRuntime) SetEventDispatchMode(mode string) error                                 { return nil }
func (m *mockRuntime) SetEventTimeout(timeout time.Duration)                                  {}
func (m *mockRuntime) SetEventWorkerPoolSize(size int)                                        {}
func (m *mockRuntime) Shutdown()                                                              {}
func (m *mockRuntime) UnregisterPrivateResource(name string) error                            { return nil }
func (m *mockRuntime) UnregisterResource(name string) error                                   { return nil }
func (m *mockRuntime) UnregisterSharedResource(name string) error                             { return nil }
func (m *mockRuntime) WithPluginContext(pluginName string) plugins.Runtime                    { return m }

type mockConfig struct {
	values map[string]any
}

func (m *mockConfig) Value(key string) config.Value {
	return &mockValue{key: key, values: m.values}
}

func (m *mockConfig) Scan(dest any) error {
	return nil
}

func (m *mockConfig) Load() error                               { return nil }
func (m *mockConfig) Watch(key string, o config.Observer) error { return nil }
func (m *mockConfig) Close() error                              { return nil }

type mockValue struct {
	key    string
	values map[string]any
}

func (m *mockValue) Scan(dest any) error {
	if val, ok := m.values[m.key]; ok {
		switch cfg := dest.(type) {
		case *interfaces.Config:
			if source, ok := val.(*interfaces.Config); ok {
				*cfg = *source
				return nil
			}
		case *conf.Mysql:
			switch source := val.(type) {
			case *conf.Mysql:
				proto.Reset(cfg)
				proto.Merge(cfg, source)
				return nil
			case *interfaces.Config:
				cfg.Driver = source.Driver
				cfg.Source = source.DSN
				cfg.MaxConn = int32(source.MaxOpenConns)
				cfg.MaxIdleConn = int32(source.MaxIdleConns)
				if source.ConnMaxLifetime > 0 {
					cfg.MaxLifeTime = durationpb.New(time.Duration(source.ConnMaxLifetime) * time.Second)
				}
				if source.ConnMaxIdleTime > 0 {
					cfg.MaxIdleTime = durationpb.New(time.Duration(source.ConnMaxIdleTime) * time.Second)
				}
				return nil
			}
		}
	}
	return nil // Return nil to use default config
}

func (m *mockValue) Bool() (bool, error)              { return false, nil }
func (m *mockValue) Int() (int64, error)              { return 0, nil }
func (m *mockValue) Float() (float64, error)          { return 0, nil }
func (m *mockValue) String() (string, error)          { return "", nil }
func (m *mockValue) Duration() (time.Duration, error) { return 0, nil }
func (m *mockValue) Slice() ([]config.Value, error)   { return nil, errors.New("not implemented") }
func (m *mockValue) Map() (map[string]config.Value, error) {
	return nil, errors.New("not implemented")
}
func (m *mockValue) Load() any   { return nil }
func (m *mockValue) Store(v any) {}

func TestNewMysqlClient(t *testing.T) {
	client := NewMysqlClient()
	if client == nil {
		t.Fatal("NewMysqlClient returned nil")
	}

	if client.Name() != pluginName {
		t.Errorf("Expected name '%s', got '%s'", pluginName, client.Name())
	}

	if client.config.Driver != "mysql" {
		t.Errorf("Expected driver 'mysql', got '%s'", client.config.Driver)
	}
}

func TestDBMysqlClient_InitializeResources(t *testing.T) {
	client := NewMysqlClient()

	config := &interfaces.Config{
		Driver:       "mysql",
		DSN:          "user:password@tcp(localhost:3306)/testdb",
		MaxOpenConns: 20,
		MaxIdleConns: 10,
	}

	rt := &mockRuntime{
		config: map[string]any{
			confPrefix: config,
		},
	}

	err := client.InitializeResources(rt)
	if err != nil {
		t.Fatalf("InitializeResources failed: %v", err)
	}

	if client.config.Driver != "mysql" {
		t.Errorf("Expected driver 'mysql', got '%s'", client.config.Driver)
	}
}

func TestDBMysqlClient_StartupTasks(t *testing.T) {
	// Skip test if MySQL is not available
	// This test requires a real MySQL connection or will fail
	t.Skip("Skipping test that requires MySQL connection")

	client := NewMysqlClient()

	config := &interfaces.Config{
		Driver:                "mysql",
		DSN:                   "root:password@tcp(localhost:3306)/testdb?charset=utf8mb4&parseTime=True",
		MaxOpenConns:          10,
		MaxIdleConns:          5,
		HealthCheckInterval:   0,
		AutoReconnectInterval: 0,
	}

	rt := &mockRuntime{
		config: map[string]any{
			confPrefix: config,
		},
	}

	err := client.InitializeResources(rt)
	if err != nil {
		t.Fatalf("InitializeResources failed: %v", err)
	}

	err = client.StartupTasks()
	if err != nil {
		t.Fatalf("StartupTasks failed: %v", err)
	}

	if !client.IsConnected() {
		t.Error("Client should be connected after StartupTasks")
	}

	// Cleanup
	_ = client.CleanupTasks()
}

func TestDBMysqlClient_CleanupTasks(t *testing.T) {
	client := NewMysqlClient()

	config := &interfaces.Config{
		Driver:                "mysql",
		DSN:                   "user:password@tcp(localhost:3306)/testdb",
		MaxOpenConns:          10,
		MaxIdleConns:          5,
		HealthCheckInterval:   0,
		AutoReconnectInterval: 0,
	}

	rt := &mockRuntime{
		config: map[string]any{
			confPrefix: config,
		},
	}

	err := client.InitializeResources(rt)
	if err != nil {
		t.Fatalf("InitializeResources failed: %v", err)
	}

	// Cleanup should not fail even if not started
	err = client.CleanupTasks()
	if err != nil {
		t.Errorf("CleanupTasks failed: %v", err)
	}
}

func TestDBMysqlClient_GetDB(t *testing.T) {
	// Test GetDB before initialization
	_, err := GetDB()
	if err == nil {
		t.Error("GetDB should return error when plugin not initialized")
	}
}

func TestDBMysqlClient_IsConnected(t *testing.T) {
	// Test IsConnected before initialization
	if IsConnected() {
		t.Error("IsConnected should return false when plugin not initialized")
	}
}

func TestDBMysqlClient_CheckHealth(t *testing.T) {
	// Test CheckHealth before initialization
	err := CheckHealth()
	if err == nil {
		t.Error("CheckHealth should return error when plugin not initialized")
	}
}

func TestDBMysqlClient_GetDialect(t *testing.T) {
	client := NewMysqlClient()

	config := &interfaces.Config{
		Driver:                "mysql",
		DSN:                   "user:password@tcp(localhost:3306)/testdb",
		MaxOpenConns:          10,
		MaxIdleConns:          5,
		HealthCheckInterval:   0,
		AutoReconnectInterval: 0,
	}

	rt := &mockRuntime{
		config: map[string]any{
			confPrefix: config,
		},
	}

	err := client.InitializeResources(rt)
	if err != nil {
		t.Fatalf("InitializeResources failed: %v", err)
	}

	dialect := GetDialect()
	if dialect != "" {
		t.Errorf("Expected empty dialect before startup, got '%s'", dialect)
	}
}

func TestDBMysqlClient_DefaultConfig(t *testing.T) {
	client := NewMysqlClient()

	if client.config.Driver != "mysql" {
		t.Errorf("Expected default driver 'mysql', got '%s'", client.config.Driver)
	}

	if client.config.MaxOpenConns != 25 {
		t.Errorf("Expected default MaxOpenConns 25, got %d", client.config.MaxOpenConns)
	}

	if client.config.MaxIdleConns != 5 {
		t.Errorf("Expected default MaxIdleConns 5, got %d", client.config.MaxIdleConns)
	}

	if client.config.ConnMaxLifetime != 3600 {
		t.Errorf("Expected default ConnMaxLifetime 3600, got %d", client.config.ConnMaxLifetime)
	}

	if client.config.HealthCheckInterval != 30 {
		t.Errorf("Expected default HealthCheckInterval 30, got %d", client.config.HealthCheckInterval)
	}
}

func TestDBMysqlClient_InitializeResources_PreservesDefaultDriver(t *testing.T) {
	client := NewMysqlClient()

	rt := &mockRuntime{
		config: map[string]any{
			confPrefix: &conf.Mysql{
				Source: "user:password@tcp(localhost:3306)/testdb",
			},
		},
	}

	if err := client.InitializeResources(rt); err != nil {
		t.Fatalf("InitializeResources failed: %v", err)
	}

	if client.config.Driver != "mysql" {
		t.Fatalf("expected default driver mysql, got %q", client.config.Driver)
	}
}

func TestDBMysqlClient_InitializeResources_RebuildsConfigFromDefaults(t *testing.T) {
	client := NewMysqlClient()

	rt1 := &mockRuntime{
		config: map[string]any{
			confPrefix: &conf.Mysql{
				Driver:      "mysql",
				Source:      "user:password@tcp(localhost:3306)/db1",
				MaxConn:     50,
				MaxIdleConn: 12,
			},
		},
	}

	if err := client.InitializeResources(rt1); err != nil {
		t.Fatalf("InitializeResources failed: %v", err)
	}

	if client.config.MaxIdleConns != 12 {
		t.Fatalf("expected MaxIdleConns 12 after first init, got %d", client.config.MaxIdleConns)
	}

	rt2 := &mockRuntime{
		config: map[string]any{
			confPrefix: &conf.Mysql{
				Source: "user:password@tcp(localhost:3306)/db2",
			},
		},
	}

	if err := client.InitializeResources(rt2); err != nil {
		t.Fatalf("InitializeResources failed on second call: %v", err)
	}

	if client.config.DSN != "user:password@tcp(localhost:3306)/db2" {
		t.Fatalf("expected DSN to be refreshed, got %q", client.config.DSN)
	}

	if client.config.MaxIdleConns != 5 {
		t.Fatalf("expected second init to reset MaxIdleConns to default 5, got %d", client.config.MaxIdleConns)
	}
}

func TestDBMysqlClient_InitializeResources_WiresMetricsRecorder(t *testing.T) {
	client := NewMysqlClient()

	rt := &mockRuntime{
		config: map[string]any{
			confPrefix: &conf.Mysql{
				Source: "user:password@tcp(localhost:3306)/testdb",
			},
		},
	}

	if err := client.InitializeResources(rt); err != nil {
		t.Fatalf("InitializeResources failed: %v", err)
	}

	if client.GetMetricsGatherer() == nil {
		t.Fatal("expected plugin metrics gatherer to be initialized")
	}

	if _, ok := client.SQLPlugin.GetMetricsRecorder().(*mysqlMetricsAdapter); !ok {
		t.Fatalf("expected mysqlMetricsAdapter, got %T", client.SQLPlugin.GetMetricsRecorder())
	}
}

func TestPrometheusMetrics_UpdateMetricsUsesCounterDeltas(t *testing.T) {
	metrics := NewPrometheusMetrics(createPrometheusConfig(&conf.Mysql{
		Driver: "mysql",
		Source: "user:password@tcp(localhost:3306)/testdb",
	}))
	cfg := &conf.Mysql{Source: "user:password@tcp(localhost:3306)/testdb"}
	stats := &base.ConnectionPoolStats{
		MaxOpenConnections: 10,
		OpenConnections:    4,
		InUse:              2,
		Idle:               2,
		MaxIdleConnections: 5,
		WaitCount:          7,
		WaitDuration:       2 * time.Second,
		MaxIdleClosed:      3,
		MaxLifetimeClosed:  4,
	}

	metrics.UpdateMetrics(stats, cfg)
	metrics.UpdateMetrics(stats, cfg)

	if got := gatherMetricValue(t, metrics.GetGatherer(), "lynx_mysql_wait_count_total"); got != 7 {
		t.Fatalf("expected wait_count counter to stay at 7 after duplicate snapshot, got %v", got)
	}
	if got := gatherMetricValue(t, metrics.GetGatherer(), "lynx_mysql_wait_duration_seconds_total"); got != 2 {
		t.Fatalf("expected wait_duration counter to stay at 2 after duplicate snapshot, got %v", got)
	}
	if got := gatherMetricValue(t, metrics.GetGatherer(), "lynx_mysql_max_idle_closed_total"); got != 3 {
		t.Fatalf("expected max_idle_closed counter to stay at 3 after duplicate snapshot, got %v", got)
	}
	if got := gatherMetricValue(t, metrics.GetGatherer(), "lynx_mysql_max_lifetime_closed_total"); got != 4 {
		t.Fatalf("expected max_lifetime_closed counter to stay at 4 after duplicate snapshot, got %v", got)
	}
}

// TestPrometheusMetrics_UpdateMetricsReflectsLatest verifies that wait_count and
// wait_duration gauges always reflect the latest DBStats cumulative total, not a sum.
// GaugeVec.Set() is used so that the gauge always mirrors the current DBStats snapshot.
func TestPrometheusMetrics_UpdateMetricsReflectsLatest(t *testing.T) {
	metrics := NewPrometheusMetrics(createPrometheusConfig(&conf.Mysql{Driver: "mysql"}))
	cfg := &conf.Mysql{}

	metrics.UpdateMetrics(&base.ConnectionPoolStats{WaitCount: 10, WaitDuration: 10 * time.Second}, cfg)
	if got := gatherMetricValue(t, metrics.GetGatherer(), "lynx_mysql_wait_count_total"); got != 10 {
		t.Fatalf("expected wait_count gauge to equal current DBStats value 10, got %v", got)
	}

	// After a second update the gauge reflects the new current total, not the accumulated sum.
	metrics.UpdateMetrics(&base.ConnectionPoolStats{WaitCount: 15, WaitDuration: 13 * time.Second}, cfg)
	if got := gatherMetricValue(t, metrics.GetGatherer(), "lynx_mysql_wait_count_total"); got != 15 {
		t.Fatalf("expected wait_count gauge to equal new DBStats value 15, got %v", got)
	}
	if got := gatherMetricValue(t, metrics.GetGatherer(), "lynx_mysql_wait_duration_seconds_total"); got != 13 {
		t.Fatalf("expected wait_duration gauge to equal new DBStats value 13, got %v", got)
	}
}

func TestDBMysqlClient_CleanupTasks_Idempotent(t *testing.T) {
	client := NewMysqlClient()

	rt := &mockRuntime{
		config: map[string]any{
			confPrefix: &conf.Mysql{
				Source: "user:password@tcp(localhost:3306)/testdb",
			},
		},
	}

	if err := client.InitializeResources(rt); err != nil {
		t.Fatalf("InitializeResources failed: %v", err)
	}

	if err := client.CleanupTasks(); err != nil {
		t.Fatalf("first CleanupTasks failed: %v", err)
	}

	if err := client.CleanupTasks(); err != nil {
		t.Fatalf("second CleanupTasks should be idempotent, got: %v", err)
	}
}

func gatherMetricValue(t *testing.T, gatherer interface {
	Gather() ([]*dto.MetricFamily, error)
}, name string) float64 {
	t.Helper()
	families, err := gatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		var total float64
		for _, metric := range family.GetMetric() {
			if metric.GetCounter() != nil {
				total += metric.GetCounter().GetValue()
			}
			if metric.GetGauge() != nil {
				total += metric.GetGauge().GetValue()
			}
		}
		return total
	}
	t.Fatalf("metric %s not found", name)
	return 0
}

func TestDBMysqlClient_ConfigurationValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *interfaces.Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &interfaces.Config{
				Driver:       "mysql",
				DSN:          "user:password@tcp(localhost:3306)/testdb",
				MaxOpenConns: 10,
				MaxIdleConns: 5,
			},
			wantErr: false,
		},
		{
			name: "missing DSN",
			config: &interfaces.Config{
				Driver:       "mysql",
				MaxOpenConns: 10,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewMysqlClient()

			rt := &mockRuntime{
				config: map[string]any{
					confPrefix: tt.config,
				},
			}

			err := client.InitializeResources(rt)
			if (err != nil) != tt.wantErr {
				t.Errorf("InitializeResources() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDBMysqlClient_ConcurrentAccess(t *testing.T) {
	client := NewMysqlClient()

	config := &interfaces.Config{
		Driver:                "mysql",
		DSN:                   "user:password@tcp(localhost:3306)/testdb",
		MaxOpenConns:          20,
		MaxIdleConns:          10,
		HealthCheckInterval:   0,
		AutoReconnectInterval: 0,
	}

	rt := &mockRuntime{
		config: map[string]any{
			confPrefix: config,
		},
	}

	err := client.InitializeResources(rt)
	if err != nil {
		t.Fatalf("InitializeResources failed: %v", err)
	}

	// Test that configuration is thread-safe
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func() {
			_ = client.config.MaxOpenConns
			done <- true
		}()
	}

	for i := 0; i < 5; i++ {
		<-done
	}
}

func TestDBMysqlClient_ContextSupport(t *testing.T) {
	client := NewMysqlClient()

	config := &interfaces.Config{
		Driver:                "mysql",
		DSN:                   "user:password@tcp(localhost:3306)/testdb",
		MaxOpenConns:          10,
		MaxIdleConns:          5,
		HealthCheckInterval:   0,
		AutoReconnectInterval: 0,
	}

	rt := &mockRuntime{
		config: map[string]any{
			confPrefix: config,
		},
	}

	err := client.InitializeResources(rt)
	if err != nil {
		t.Fatalf("InitializeResources failed: %v", err)
	}

	// Test with context
	ctx := context.Background()
	_, err = client.GetDBWithContext(ctx)
	if err == nil {
		t.Error("GetDBWithContext should return error when not connected")
	}

	// Test with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.GetDBWithContext(ctx)
	if err == nil {
		t.Error("GetDBWithContext should return error with cancelled context")
	}
}

func TestDBMysqlClient_TimeoutHandling(t *testing.T) {
	client := NewMysqlClient()

	config := &interfaces.Config{
		Driver:                "mysql",
		DSN:                   "user:password@tcp(localhost:3306)/testdb",
		MaxOpenConns:          10,
		MaxIdleConns:          5,
		HealthCheckInterval:   0,
		AutoReconnectInterval: 0,
	}

	rt := &mockRuntime{
		config: map[string]any{
			confPrefix: config,
		},
	}

	err := client.InitializeResources(rt)
	if err != nil {
		t.Fatalf("InitializeResources failed: %v", err)
	}

	// Test with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = client.GetDBWithContext(ctx)
	if err == nil {
		t.Error("GetDBWithContext should return error when not connected")
	}
}

func TestDBMysqlClient_PluginMetadata(t *testing.T) {
	client := NewMysqlClient()

	if client.ID() == "" {
		t.Error("Plugin ID should not be empty")
	}

	if client.Name() != pluginName {
		t.Errorf("Expected plugin name '%s', got '%s'", pluginName, client.Name())
	}

	if client.Version() != pluginVersion {
		t.Errorf("Expected plugin version '%s', got '%s'", pluginVersion, client.Version())
	}

	if client.Description() != pluginDescription {
		t.Errorf("Expected plugin description '%s', got '%s'", pluginDescription, client.Description())
	}
}

// TestDBMysqlClient_ConcurrentInitializeResources verifies that concurrent calls to
// InitializeResources do not race on shared fields (run with -race).
func TestDBMysqlClient_ConcurrentInitializeResources(t *testing.T) {
	const workers = 8
	rt := &mockRuntime{
		config: map[string]any{
			confPrefix: &conf.Mysql{
				Source:  "user:password@tcp(localhost:3306)/testdb",
				MaxConn: 20,
			},
		},
	}

	client := NewMysqlClient()
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			errs <- client.InitializeResources(rt)
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent InitializeResources error: %v", err)
		}
	}
}

// TestDBMysqlClient_CleanupTasksIsIdempotent verifies that CleanupTasks can be
// called multiple times without error (pool stats reporter is managed by base).
func TestDBMysqlClient_CleanupTasksIsIdempotent(t *testing.T) {
	client := NewMysqlClient()
	rt := &mockRuntime{
		config: map[string]any{
			confPrefix: &conf.Mysql{
				Source:  "user:password@tcp(localhost:3306)/testdb",
				MaxConn: 10,
			},
		},
	}
	if err := client.InitializeResources(rt); err != nil {
		t.Fatalf("InitializeResources: %v", err)
	}

	// CleanupTasks on an unstarted plugin should be a no-op.
	if err := client.CleanupTasks(); err != nil {
		t.Fatalf("first CleanupTasks: %v", err)
	}
	if err := client.CleanupTasks(); err != nil {
		t.Fatalf("second CleanupTasks should be idempotent: %v", err)
	}
}

// TestDBMysqlClient_PrometheusMetricsGatherer verifies that after InitializeResources
// the plugin exposes a non-nil Prometheus gatherer and that the gatherer works after
// metrics are recorded.
func TestDBMysqlClient_PrometheusMetricsGatherer(t *testing.T) {
	pbCfg := &conf.Mysql{
		Source:  "user:password@tcp(localhost:3306)/testdb",
		MaxConn: 10,
	}
	client := NewMysqlClient()
	rt := &mockRuntime{
		config: map[string]any{confPrefix: pbCfg},
	}
	if err := client.InitializeResources(rt); err != nil {
		t.Fatalf("InitializeResources: %v", err)
	}

	gatherer := client.GetMetricsGatherer()
	if gatherer == nil {
		t.Fatal("expected non-nil Prometheus gatherer after InitializeResources")
	}

	// Seed at least one observation so metric families are emitted by the gatherer.
	client.mu.RLock()
	pm := client.prometheusMetrics
	client.mu.RUnlock()
	if pm == nil {
		t.Fatal("expected prometheusMetrics to be non-nil after InitializeResources")
	}
	pm.UpdateMetrics(&base.ConnectionPoolStats{
		MaxOpenConnections: 10,
		OpenConnections:    1,
		InUse:              1,
	}, pbCfg)

	families, err := gatherer.Gather()
	if err != nil {
		t.Fatalf("Gather() returned error: %v", err)
	}
	if len(families) == 0 {
		t.Error("expected at least one metric family from the Prometheus gatherer after UpdateMetrics")
	}
}

func TestExtractInstance_MySQL(t *testing.T) {
	pm := &PrometheusMetrics{}
	tests := []struct {
		name     string
		dsn      string
		wantInst string
	}{
		{
			name:     "tcp protocol explicit",
			dsn:      "user:pass@tcp(127.0.0.1:3306)/mydb",
			wantInst: "127.0.0.1:3306",
		},
		{
			name:     "unix socket",
			dsn:      "user:pass@unix(/tmp/mysql.sock)/mydb",
			wantInst: "/tmp/mysql.sock",
		},
		{
			name:     "no explicit protocol",
			dsn:      "user:pass@127.0.0.1:3306/mydb",
			wantInst: "127.0.0.1:3306",
		},
		{
			name:     "no user info",
			dsn:      "127.0.0.1:3306/mydb",
			wantInst: "127.0.0.1:3306",
		},
		{
			name:     "tcp localhost default port",
			dsn:      "root:secret@tcp(localhost:3306)/testdb",
			wantInst: "localhost:3306",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pm.extractInstance(tc.dsn); got != tc.wantInst {
				t.Errorf("extractInstance(%q) = %q, want %q", tc.dsn, got, tc.wantInst)
			}
		})
	}
}

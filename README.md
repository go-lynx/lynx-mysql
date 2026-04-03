# MySQL Plugin for Lynx Framework

The MySQL Plugin provides comprehensive MySQL database integration for the Lynx framework, supporting both standalone and cluster deployments with advanced features like connection pooling, health monitoring, and performance optimization.

## Features

### Core Database Support
- **Full MySQL Compatibility**: Support for MySQL 5.7+ and MariaDB 10.3+
- **Connection Pooling**: Efficient connection management with configurable pool sizes
- **Transaction Support**: Full ACID transaction support with configurable isolation levels
- **Prepared Statements**: Optimized prepared statement support
- **Multiple Charsets**: Support for UTF-8, UTF-8MB4, and other character sets

### Security Features
- **SSL/TLS Encryption**: Secure connections with certificate validation
- **Authentication**: Username/password and certificate-based authentication
- **Connection Security**: IP whitelisting and secure connection strings
- **Data Encryption**: At-rest and in-transit data encryption support

### Performance & Monitoring
- **Prometheus Metrics**: Comprehensive monitoring and alerting
- **Connection Statistics**: Real-time connection pool monitoring
- **Query Performance**: Query execution time and performance tracking
- **Health Checks**: Automated health monitoring and reporting
- **Slow Query Logging**: Configurable slow query detection and logging

### Configuration Management
- **Flexible Configuration**: YAML and JSON configuration support
- **Environment-Specific**: Different configurations for dev, staging, and production
- **Hot Reloading**: Runtime configuration updates without restart
- **Validation**: Automatic configuration validation and error reporting

## Architecture

The plugin follows the Lynx framework's layered architecture:

```
┌─────────────────────────────────────────────────────────────┐
│                    Application Layer                        │
├─────────────────────────────────────────────────────────────┤
│                    MySQL Plugin Layer                       │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │   Client    │  │   Metrics   │  │   Configuration    │ │
│  └─────────────┘  └─────────────┘  └─────────────────────┘ │
├─────────────────────────────────────────────────────────────┤
│                    Base SQL Layer                           │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │ Connection  │  │   Pool      │  │   Health Check      │ │
│  │   Pool      │  │ Management  │  │     System          │ │
│  └─────────────┘  └─────────────┘  └─────────────────────┘ │
├─────────────────────────────────────────────────────────────┤
│                    Driver Layer                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │   MySQL     │  │   Ent       │  │   Database/SQL     │ │
│  │   Driver    │  │   Driver    │  │     Interface      │ │
│  └─────────────┘  └─────────────┘  └─────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

## Configuration

### Basic Configuration

```yaml
lynx:
  mysql:
    driver: "mysql"
    source: "user:password@tcp(localhost:3306)/database?charset=utf8mb4&parseTime=True&loc=Local"
    min_conn: 5
    max_conn: 20
    max_idle_conn: 5
    max_idle_time: 300s
    max_life_time: 3600s
```

### Advanced Configuration

```yaml
lynx:
  mysql:
    driver: "mysql"
    source: "user:password@tcp(db.internal:3306)/app?charset=utf8mb4&parseTime=True&loc=Local&tls=true&timeout=10s&readTimeout=30s&writeTimeout=30s"
    min_conn: 10
    max_conn: 100
    max_idle_conn: 20
    max_idle_time: 300s
    max_life_time: 3600s
```

The protobuf schema uses `source` as the DSN field name. Some config loaders may also accept `dsn` as an alias, but `source` is the canonical key defined in `conf/mysql.proto`.

### Proto Configuration Reference

| Field | Proto Type | Default Value | Example | Notes |
|-------|------------|---------------|---------|-------|
| `driver` | `string` | `"mysql"` | `"mysql"` | MySQL driver name used by the plugin. |
| `source` | `string` | required | `"user:password@tcp(localhost:3306)/app?charset=utf8mb4&parseTime=True&loc=Local"` | Standard MySQL DSN. Put charset, TLS, and timeout tuning in DSN query parameters. |
| `min_conn` | `int32` | `0` | `5` | When greater than `0`, the plugin warms up that many idle connections. If omitted, the runtime keeps the plugin default idle pool size of `5` without warmup. |
| `max_conn` | `int32` | `25` | `20` | Maximum number of open connections. |
| `max_life_time` | `google.protobuf.Duration` | `"3600s"` | `"3600s"` | Maximum lifetime of a pooled connection. |
| `max_idle_time` | `google.protobuf.Duration` | `"300s"` | `"300s"` | Maximum idle lifetime of a pooled connection. |
| `max_idle_conn` | `int32` | `0` | `10` | Explicit idle connection cap. Overrides `min_conn` for idle pool sizing when set. |

Durations use protobuf/Kratos duration syntax such as `"30s"`, `"5m"`, or `"1h"`.

## Usage

### Basic Usage

```go
package main

import (
    "context"
    "database/sql"
    "github.com/go-lynx/lynx/plugins/sql/mysql"
)

func main() {
    // Get the MySQL client instance
    mysqlClient := mysql.GetMysqlClient()
    
    // Get the underlying database connection
    db := mysql.GetMysqlDB()
    
    // Execute a simple query
    ctx := context.Background()
    var result string
    err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&result)
    if err != nil {
        panic(err)
    }
    
    fmt.Printf("MySQL Version: %s\n", result)
}
```

### Advanced Usage

```go
// Test connection
err := mysqlClient.TestConnection(ctx)
if err != nil {
    log.Printf("Connection test failed: %v", err)
}

// Get server information
serverInfo, err := mysqlClient.GetServerInfo(ctx)
if err != nil {
    log.Printf("Failed to get server info: %v", err)
} else {
    log.Printf("Server: %s, Version: %s", 
        serverInfo["hostname"], serverInfo["version"])
}

// Execute prepared statement
stmt, err := mysqlClient.PrepareStatement(ctx, "SELECT * FROM users WHERE id = ?")
if err != nil {
    log.Printf("Failed to prepare statement: %v", err)
}
defer stmt.Close()

// Begin transaction
tx, err := mysqlClient.BeginTransaction(ctx)
if err != nil {
    log.Printf("Failed to begin transaction: %v", err)
}
defer tx.Rollback()

// Get connection statistics
stats := mysqlClient.GetConnectionStats()
log.Printf("Active connections: %d, Idle: %d", 
    stats["in_use"], stats["idle"])
```

### Health Monitoring

```go
// Check plugin health
err := mysqlClient.CheckHealth()
if err != nil {
    log.Printf("Health check failed: %v", err)
}

// Check connection status
if mysqlClient.IsConnected() {
    log.Println("MySQL plugin is connected")
} else {
    log.Println("MySQL plugin is not connected")
}

// Get detailed health status
health := mysqlClient.GetHealthStatus()
log.Printf("Health: %s, Last Check: %v", 
    health.Status, health.LastChecked)
```

## API Reference

### MySQLClient

The main client interface providing access to all MySQL functionality.

#### Methods

- `GetMysqlConfig() *conf.Mysql` - Returns the current configuration
- `TestConnection(ctx context.Context) error` - Tests database connectivity
- `GetServerInfo(ctx context.Context) (map[string]interface{}, error)` - Gets server information
- `PrepareStatement(ctx context.Context, query string) (*sql.Stmt, error)` - Prepares a statement
- `BeginTransaction(ctx context.Context) (*sql.Tx, error)` - Begins a new transaction
- `IsConnected() bool` - Checks connection status
- `GetConnectionStats() map[string]interface{}` - Gets connection statistics
- `CheckHealth() error` - Performs health check
- `GetHealthStatus() *HealthStatus` - Gets detailed health status

### Configuration

See `conf/mysql.go` for detailed configuration structure definitions.

## Connection String Format

The plugin supports standard MySQL DSN format:

```
[username[:password]@][protocol[(address)]]/dbname[?param1=value1&...&paramN=valueN]
```

### Common Parameters

- `charset`: Character set (utf8, utf8mb4, etc.)
- `parseTime`: Parse time values to time.Time
- `loc`: Location for time parsing
- `tls`: TLS configuration (true, false, skip-verify, preferred)
- `timeout`: Connection timeout
- `readTimeout`: Read timeout
- `writeTimeout`: Write timeout
- `collation`: Collation setting

### Example Connection Strings

```go
// Basic connection
"user:password@tcp(localhost:3306)/database"

// With charset and time parsing
"user:password@tcp(localhost:3306)/database?charset=utf8mb4&parseTime=True&loc=Local"

// With SSL
"user:password@tcp(localhost:3306)/database?tls=true"

// With timeouts
"user:password@tcp(localhost:3306)/database?timeout=10s&readTimeout=30s&writeTimeout=30s"
```

## Monitoring and Metrics

### Prometheus Metrics

The plugin exposes comprehensive Prometheus metrics:

#### Connection Pool Metrics
- `lynx_mysql_connection_pool_max_open` - Maximum open connections
- `lynx_mysql_connection_pool_open` - Current open connections
- `lynx_mysql_connection_pool_in_use` - Connections in use
- `lynx_mysql_connection_pool_idle` - Idle connections
- `lynx_mysql_connection_pool_wait_count_total` - Total wait count
- `lynx_mysql_connection_pool_wait_duration_seconds` - Wait duration

#### Health Check Metrics
- `lynx_mysql_health_check_total` - Total health checks
- `lynx_mysql_health_check_success_total` - Successful health checks
- `lynx_mysql_health_check_failure_total` - Failed health checks

#### Query Performance Metrics
- `lynx_mysql_query_duration_seconds` - Query execution duration
- `lynx_mysql_slow_queries_total` - Slow query count
- `lynx_mysql_query_errors_total` - Query error count

#### Configuration Metrics
- `lynx_mysql_config_max_connections` - Configured max connections
- `lynx_mysql_config_min_connections` - Configured min connections
- `lynx_mysql_config_ssl_enabled` - SSL status

### Grafana Dashboard

The plugin includes a Grafana dashboard for monitoring:

```json
{
  "dashboard": {
    "title": "Lynx MySQL Plugin",
    "panels": [
      {
        "title": "Connection Pool Status",
        "type": "stat",
        "targets": [
          {
            "expr": "lynx_mysql_connection_pool_open",
            "legendFormat": "Open Connections"
          }
        ]
      },
      {
        "title": "Query Performance",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(lynx_mysql_query_duration_seconds[5m])",
            "legendFormat": "Query Rate"
          }
        ]
      }
    ]
  }
}
```

## Deployment

### Local Development

```bash
cd plugins/sql/mysql
go mod tidy
go build
```

### Docker Deployment

```dockerfile
FROM mysql:8.0

# Install Go and build the plugin
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod download && go build -o mysql-plugin .

FROM mysql:8.0
COPY --from=builder /app/mysql-plugin /usr/local/bin/
CMD ["mysql-plugin"]
```

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: lynx-mysql-plugin
spec:
  replicas: 1
  selector:
    matchLabels:
      app: lynx-mysql-plugin
  template:
    metadata:
      labels:
        app: lynx-mysql-plugin
    spec:
      containers:
      - name: mysql-plugin
        image: lynx-mysql-plugin:latest
        ports:
        - containerPort: 8080
        env:
        - name: MYSQL_HOST
          value: "mysql-service"
        - name: MYSQL_DATABASE
          value: "your_database"
        - name: MYSQL_USER
          valueFrom:
            secretKeyRef:
              name: mysql-secret
              key: username
        - name: MYSQL_PASSWORD
          valueFrom:
            secretKeyRef:
              name: mysql-secret
              key: password
```

## Troubleshooting

### Common Issues

1. **Connection Failed**
   - Check MySQL service status
   - Verify connection parameters
   - Check firewall settings
   - Ensure MySQL user has proper permissions

2. **Authentication Errors**
   - Verify username/password
   - Check user privileges
   - Verify host access permissions

3. **Connection Pool Issues**
   - Check max_conn and min_conn settings
   - Monitor connection pool metrics
   - Check for connection leaks

4. **Performance Issues**
   - Monitor query performance metrics
   - Check connection pool utilization
   - Review timeout settings
   - Analyze slow query logs

5. **SSL/TLS Issues**
   - Verify certificate files
   - Check SSL configuration
   - Ensure proper certificate permissions

### Debug Mode

Enable debug logging for detailed troubleshooting:

```yaml
lynx:
  mysql:
    dsn: "user:password@tcp(localhost:3306)/database?debug=true"
    monitoring:
      enable_slow_query_log: true
      slow_query_threshold: 100ms
```

### Health Check Troubleshooting

```go
// Check detailed health status
health := mysqlClient.GetHealthStatus()
if !health.Healthy {
    log.Printf("Health check failed: %s", health.Error)
    log.Printf("Last successful check: %v", health.LastSuccess)
    log.Printf("Check interval: %v", health.CheckInterval)
}
```

## Best Practices

### Connection Management
- Use appropriate pool sizes for your workload
- Monitor connection pool metrics regularly
- Implement proper error handling and retry logic
- Use connection timeouts appropriately

### Security
- Use SSL/TLS encryption in production
- Implement proper authentication
- Regularly rotate database passwords
- Use least privilege principle for database users

### Performance
- Use connection pooling effectively
- Monitor query performance
- Implement proper indexing strategies
- Use prepared statements for repeated queries

### Monitoring
- Set up Prometheus alerts for critical metrics
- Monitor connection pool utilization
- Track query performance trends
- Set up slow query monitoring

### Configuration
- Use environment-specific configurations
- Validate configurations before deployment
- Use secure connection strings
- Implement proper backup strategies

## Contributing

Contributions are welcome! Please see the main Lynx framework contribution guidelines.

## License

This plugin is part of the Lynx framework and follows the same license terms.

## Support

For support and questions:
- GitHub Issues: [Lynx Framework Issues](https://github.com/go-lynx/lynx/issues)
- Documentation: [Lynx Documentation](https://lynx.go-lynx.com)
- Community: [Lynx Community](https://community.go-lynx.com)

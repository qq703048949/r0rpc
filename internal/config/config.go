package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var configSearchPaths = []string{
	filepath.Join("deploy", "linux", ".env.docker"),
	"r0rpc.conf",
}

type Config struct {
	AppName                  string
	ServerID                 string
	HTTPAddr                 string
	JWTSecret                string
	RequestTimeout           time.Duration
	RawRetentionDays         int
	AggregateRetentionDays   int
	DeviceOfflineSeconds     int
	DeviceOfflineMinutes     int
	HeartbeatIntervalSeconds int
	PresenceFlushSeconds     int
	PersistQueueSize         int
	PersistWorkers           int
	ClientQueueSize          int
	ClientMaxInFlight        int
	TimeZone                 string
	BootstrapAdminUser       string
	BootstrapAdminPass       string
	MySQL                    MySQLConfig
	Redis                    RedisConfig
}

type MySQLConfig struct {
	Host                   string
	Port                   int
	User                   string
	Password               string
	DB                     string
	Params                 string
	MaxOpenConns           int
	MaxIdleConns           int
	ConnMaxLifetimeMinutes int
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

func Load() (Config, error) {
	values, path, err := loadConfigFromSearchPaths()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		AppName:                  getString(values, "APP_NAME", "r0rpc-demo"),
		ServerID:                 getString(values, "SERVER_ID", "r0rpc-node-1"),
		HTTPAddr:                 getString(values, "HTTP_ADDR", ":8080"),
		JWTSecret:                getString(values, "JWT_SECRET", ""),
		RequestTimeout:           time.Duration(getInt(values, "REQUEST_TIMEOUT_SECONDS", 25)) * time.Second,
		RawRetentionDays:         getInt(values, "RAW_RETENTION_DAYS", 3),
		AggregateRetentionDays:   getInt(values, "AGGREGATE_RETENTION_DAYS", 30),
		DeviceOfflineSeconds:     getInt(values, "DEVICE_OFFLINE_SECONDS", 20),
		DeviceOfflineMinutes:     getInt(values, "DEVICE_OFFLINE_MINUTES", 2),
		HeartbeatIntervalSeconds: getInt(values, "HEARTBEAT_INTERVAL_SECONDS", 5),
		PresenceFlushSeconds:     getInt(values, "PRESENCE_FLUSH_SECONDS", 5),
		PersistQueueSize:         getInt(values, "PERSIST_QUEUE_SIZE", 131072),
		PersistWorkers:           getInt(values, "PERSIST_WORKERS", 32),
		ClientQueueSize:          getInt(values, "CLIENT_QUEUE_SIZE", 2048),
		ClientMaxInFlight:        getInt(values, "CLIENT_MAX_IN_FLIGHT", 256),
		TimeZone:                 getString(values, "TIME_ZONE", "Asia/Shanghai"),
		BootstrapAdminUser:       getString(values, "BOOTSTRAP_ADMIN_USERNAME", "admin"),
		BootstrapAdminPass:       getString(values, "BOOTSTRAP_ADMIN_PASSWORD", "123456"),
		MySQL: MySQLConfig{
			Host:                   getString(values, "MYSQL_HOST", "101.33.243.78"),
			Port:                   getInt(values, "MYSQL_PORT", 3306),
			User:                   getString(values, "MYSQL_USER", "root"),
			Password:               getString(values, "MYSQL_PASSWORD", "QiLongZhuDamo!@"),
			DB:                     getString(values, "MYSQL_DB", "r0rpc"),
			Params:                 getString(values, "MYSQL_PARAMS", "charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai&timeout=5s&readTimeout=30s&writeTimeout=30s"),
			MaxOpenConns:           getInt(values, "MYSQL_MAX_OPEN_CONNS", 256),
			MaxIdleConns:           getInt(values, "MYSQL_MAX_IDLE_CONNS", 64),
			ConnMaxLifetimeMinutes: getInt(values, "MYSQL_CONN_MAX_LIFETIME_MINUTES", 10),
		},
		Redis: RedisConfig{
			Addr:     getString(values, "REDIS_ADDR", "101.33.243.78:6380"),
			Password: getString(values, "REDIS_PASSWORD", "QiLongZhuDamo!@"),
			DB:       getInt(values, "REDIS_DB", 8),
		},
	}

	if strings.TrimSpace(cfg.JWTSecret) == "" {
		return Config{}, fmt.Errorf("JWT_SECRET is required in %s", path)
	}
	if cfg.RawRetentionDays <= 0 {
		cfg.RawRetentionDays = 3
	}
	if cfg.AggregateRetentionDays <= 0 {
		cfg.AggregateRetentionDays = 30
	}
	if cfg.DeviceOfflineSeconds <= 0 {
		if cfg.DeviceOfflineMinutes > 0 {
			cfg.DeviceOfflineSeconds = cfg.DeviceOfflineMinutes * 60
		} else {
			cfg.DeviceOfflineSeconds = 20
		}
	}
	cfg.DeviceOfflineMinutes = cfg.DeviceOfflineSeconds / 60
	if cfg.HeartbeatIntervalSeconds <= 0 {
		cfg.HeartbeatIntervalSeconds = 5
	}
	if cfg.HeartbeatIntervalSeconds >= cfg.DeviceOfflineSeconds {
		cfg.HeartbeatIntervalSeconds = maxInt(1, cfg.DeviceOfflineSeconds/2)
	}
	if cfg.PresenceFlushSeconds <= 0 {
		cfg.PresenceFlushSeconds = minInt(5, cfg.DeviceOfflineSeconds)
	}
	if cfg.PresenceFlushSeconds >= cfg.DeviceOfflineSeconds {
		cfg.PresenceFlushSeconds = maxInt(1, cfg.DeviceOfflineSeconds/2)
	}
	if cfg.PersistQueueSize < 131072 {
		cfg.PersistQueueSize = 131072
	}
	if cfg.PersistWorkers < 32 {
		cfg.PersistWorkers = 32
	}
	if cfg.ClientQueueSize < 2048 {
		cfg.ClientQueueSize = 2048
	}
	if cfg.ClientMaxInFlight < 256 {
		cfg.ClientMaxInFlight = 256
	}
	if cfg.MySQL.MaxOpenConns < 256 {
		cfg.MySQL.MaxOpenConns = 256
	}
	if cfg.MySQL.MaxIdleConns < 64 {
		cfg.MySQL.MaxIdleConns = 64
	}
	if cfg.MySQL.ConnMaxLifetimeMinutes <= 0 {
		cfg.MySQL.ConnMaxLifetimeMinutes = 10
	}
	if cfg.TimeZone == "" {
		cfg.TimeZone = "Asia/Shanghai"
	}
	return cfg, nil
}

func (c Config) MySQLDSN(withoutDB bool) string {
	dbName := c.MySQL.DB
	if withoutDB {
		dbName = ""
	}
	if dbName != "" {
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s", c.MySQL.User, c.MySQL.Password, c.MySQL.Host, c.MySQL.Port, dbName, c.MySQL.Params)
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/?%s", c.MySQL.User, c.MySQL.Password, c.MySQL.Host, c.MySQL.Port, c.MySQL.Params)
}

func (c Config) LoadLocation() (*time.Location, error) {
	return time.LoadLocation(c.TimeZone)
}

func (c Config) ApplyTimeZone() error {
	loc, err := c.LoadLocation()
	if err != nil {
		return err
	}
	time.Local = loc
	return nil
}

func (c Config) TimeZoneOffsetString() string {
	loc, err := c.LoadLocation()
	if err != nil {
		loc = time.FixedZone("UTC+8", 8*3600)
	}
	_, offsetSeconds := time.Now().In(loc).Zone()
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}
	hours := offsetSeconds / 3600
	minutes := (offsetSeconds % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, hours, minutes)
}

func loadConfigFromSearchPaths() (map[string]string, string, error) {
	searched := make([]string, 0, len(configSearchPaths))
	for _, candidate := range configSearchPaths {
		path, err := filepath.Abs(candidate)
		if err != nil {
			path = candidate
		}
		searched = append(searched, path)
		if _, err := os.Stat(candidate); err == nil {
			values, parsedPath, parseErr := parseConfigFile(candidate)
			if parseErr != nil {
				return nil, parsedPath, parseErr
			}
			return values, parsedPath, nil
		}
	}
	return nil, "", fmt.Errorf("no config file found, searched: %s", strings.Join(searched, ", "))
}

func parseConfigFile(name string) (map[string]string, string, error) {
	path, err := filepath.Abs(name)
	if err != nil {
		path = name
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, path, fmt.Errorf("open config file %s: %w", path, err)
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, path, fmt.Errorf("invalid config line %d in %s", lineNo, path)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, "\"'")
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, path, fmt.Errorf("read config file %s: %w", path, err)
	}
	return values, path, nil
}

func getString(values map[string]string, key, fallback string) string {
	if value := strings.TrimSpace(values[key]); value != "" {
		return value
	}
	return fallback
}

func getInt(values map[string]string, key string, fallback int) int {
	if value := strings.TrimSpace(values[key]); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

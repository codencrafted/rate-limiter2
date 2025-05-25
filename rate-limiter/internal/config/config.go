package config

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration for the service
type Config struct {
	Redis       RedisConfig       `mapstructure:"redis"`
	HTTP        HTTPConfig        `mapstructure:"http"`
	GRPC        GRPCConfig        `mapstructure:"grpc"`
	RateLimiter RateLimiterConfig `mapstructure:"rate_limiter"`
	Auth        AuthConfig        `mapstructure:"auth"`
	Metrics     MetricsConfig     `mapstructure:"metrics"`
	Tracing     TracingConfig     `mapstructure:"tracing"`
	Logging     LoggingConfig     `mapstructure:"logging"`
}

// RedisConfig holds Redis-specific configuration
type RedisConfig struct {
	Addresses    []string      `mapstructure:"addresses"`
	Password     string        `mapstructure:"password"`
	DB           int           `mapstructure:"db"`
	PoolSize     int           `mapstructure:"pool_size"`
	MinIdleConns int           `mapstructure:"min_idle_conns"`
	MaxRetries   int           `mapstructure:"max_retries"`
	DialTimeout  time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	PoolTimeout  time.Duration `mapstructure:"pool_timeout"`
	ClusterMode  bool          `mapstructure:"cluster_mode"`
}

// HTTPConfig holds HTTP server configuration
type HTTPConfig struct {
	Address           string        `mapstructure:"address"`
	ReadTimeout       time.Duration `mapstructure:"read_timeout"`
	WriteTimeout      time.Duration `mapstructure:"write_timeout"`
	ShutdownTimeout   time.Duration `mapstructure:"shutdown_timeout"`
	MaxHeaderBytes    int           `mapstructure:"max_header_bytes"`
	EnableCORS        bool          `mapstructure:"enable_cors"`
	EnableCompression bool          `mapstructure:"enable_compression"`
	TLS               TLSConfig     `mapstructure:"tls"`
}

// TLSConfig holds TLS configuration
type TLSConfig struct {
	Enabled     bool   `mapstructure:"enabled"`
	CertFile    string `mapstructure:"cert_file"`
	KeyFile     string `mapstructure:"key_file"`
	MinVersion  string `mapstructure:"min_version"`
	MaxVersion  string `mapstructure:"max_version"`
	CipherSuites string `mapstructure:"cipher_suites"`
}

// GRPCConfig holds gRPC server configuration
type GRPCConfig struct {
	Address         string        `mapstructure:"address"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	MaxRecvMsgSize  int           `mapstructure:"max_recv_msg_size"`
	MaxSendMsgSize  int           `mapstructure:"max_send_msg_size"`
	TLS             TLSConfig     `mapstructure:"tls"`
}

// RateLimiterConfig holds rate limiter specific configuration
type RateLimiterConfig struct {
	DefaultLimits       map[string]Limit `mapstructure:"default_limits"`
	Whitelist           []string         `mapstructure:"whitelist"`
	CircuitBreakerThreshold int          `mapstructure:"circuit_breaker_threshold"`
	CircuitBreakerTimeout   time.Duration `mapstructure:"circuit_breaker_timeout"`
	LocalCacheEnabled       bool          `mapstructure:"local_cache_enabled"`
	LocalCacheSize          int           `mapstructure:"local_cache_size"`
	LocalCacheTTL           time.Duration `mapstructure:"local_cache_ttl"`
}

// Limit defines a rate limit for a specific window
type Limit struct {
	Requests int           `mapstructure:"requests"`
	Window   time.Duration `mapstructure:"window"`
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	Enabled       bool          `mapstructure:"enabled"`
	APIKeys       []APIKeyConfig `mapstructure:"api_keys"`
	JWTSecret     string        `mapstructure:"jwt_secret"`
	JWTExpiration time.Duration `mapstructure:"jwt_expiration"`
	BasicAuth     BasicAuthConfig `mapstructure:"basic_auth"`
}

// APIKeyConfig defines an API key configuration
type APIKeyConfig struct {
	Key      string `mapstructure:"key"`
	Role     string `mapstructure:"role"`
	IsActive bool   `mapstructure:"is_active"`
}

// BasicAuthConfig holds basic auth configuration
type BasicAuthConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

// MetricsConfig holds metrics configuration
type MetricsConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	Address       string `mapstructure:"address"`
	Path          string `mapstructure:"path"`
	EnableGoStats bool   `mapstructure:"enable_go_stats"`
}

// TracingConfig holds tracing configuration
type TracingConfig struct {
	Enabled     bool    `mapstructure:"enabled"`
	ServiceName string  `mapstructure:"service_name"`
	Endpoint    string  `mapstructure:"endpoint"`
	SamplingRate float64 `mapstructure:"sampling_rate"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level      string `mapstructure:"level"`
	Format     string `mapstructure:"format"`
	Output     string `mapstructure:"output"`
	TimeFormat string `mapstructure:"time_format"`
	EnableCaller bool  `mapstructure:"enable_caller"`
}

// Load loads configuration from file and environment variables
func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")
	viper.AddConfigPath("/etc/ratelimiter")
	
	// Set defaults
	setDefaults()
	
	// Environment variables will override configuration file settings
	viper.SetEnvPrefix("RATELIMITER")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	
	// Allow override of specific config values via environment variables
	// For example: RATELIMITER_REDIS_PASSWORD will override redis.password
	bindEnvVariables()
	
	// Read config file
	if err := viper.ReadInConfig(); err != nil {
		// It's okay if config file doesn't exist
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}
	
	config := &Config{}
	if err := viper.Unmarshal(config); err != nil {
		return nil, err
	}
	
	// Override with environment variables for sensitive information
	loadSecretsFromEnv(config)
	
	return config, nil
}

// setDefaults sets default values for configuration
func setDefaults() {
	// Redis defaults
	viper.SetDefault("redis.addresses", []string{"localhost:6379"})
	viper.SetDefault("redis.password", "")
	viper.SetDefault("redis.db", 0)
	viper.SetDefault("redis.pool_size", 100)
	viper.SetDefault("redis.min_idle_conns", 10)
	viper.SetDefault("redis.max_retries", 3)
	viper.SetDefault("redis.dial_timeout", 200 * time.Millisecond)
	viper.SetDefault("redis.read_timeout", 500 * time.Millisecond)
	viper.SetDefault("redis.write_timeout", 500 * time.Millisecond)
	viper.SetDefault("redis.pool_timeout", 1 * time.Second)
	viper.SetDefault("redis.cluster_mode", false)
	
	// HTTP defaults
	viper.SetDefault("http.address", ":8080")
	viper.SetDefault("http.read_timeout", 5 * time.Second)
	viper.SetDefault("http.write_timeout", 10 * time.Second)
	viper.SetDefault("http.shutdown_timeout", 30 * time.Second)
	viper.SetDefault("http.max_header_bytes", 1 << 20) // 1 MB
	viper.SetDefault("http.enable_cors", true)
	viper.SetDefault("http.enable_compression", true)
	
	// TLS defaults
	viper.SetDefault("http.tls.enabled", false)
	viper.SetDefault("http.tls.cert_file", "server.crt")
	viper.SetDefault("http.tls.key_file", "server.key")
	viper.SetDefault("http.tls.min_version", "1.2")
	viper.SetDefault("http.tls.max_version", "")
	viper.SetDefault("http.tls.cipher_suites", "")
	
	// gRPC defaults
	viper.SetDefault("grpc.address", ":9090")
	viper.SetDefault("grpc.shutdown_timeout", 30 * time.Second)
	viper.SetDefault("grpc.max_recv_msg_size", 4 * 1024 * 1024) // 4 MB
	viper.SetDefault("grpc.max_send_msg_size", 4 * 1024 * 1024) // 4 MB
	viper.SetDefault("grpc.tls.enabled", false)
	
	// Default rate limits
	viper.SetDefault("rate_limiter.default_limits.second.requests", 10)
	viper.SetDefault("rate_limiter.default_limits.second.window", time.Second)
	viper.SetDefault("rate_limiter.default_limits.minute.requests", 100)
	viper.SetDefault("rate_limiter.default_limits.minute.window", time.Minute)
	viper.SetDefault("rate_limiter.default_limits.hour.requests", 1000)
	viper.SetDefault("rate_limiter.default_limits.hour.window", time.Hour)
	viper.SetDefault("rate_limiter.default_limits.day.requests", 10000)
	viper.SetDefault("rate_limiter.default_limits.day.window", 24*time.Hour)
	
	// Rate limiter defaults
	viper.SetDefault("rate_limiter.circuit_breaker_threshold", 5)
	viper.SetDefault("rate_limiter.circuit_breaker_timeout", 10 * time.Second)
	viper.SetDefault("rate_limiter.local_cache_enabled", true)
	viper.SetDefault("rate_limiter.local_cache_size", 10000)
	viper.SetDefault("rate_limiter.local_cache_ttl", 1 * time.Minute)
	
	// Auth defaults
	viper.SetDefault("auth.enabled", false)
	viper.SetDefault("auth.jwt_expiration", 24 * time.Hour)
	viper.SetDefault("auth.basic_auth.enabled", false)
	
	// Metrics defaults
	viper.SetDefault("metrics.enabled", true)
	viper.SetDefault("metrics.address", ":9091")
	viper.SetDefault("metrics.path", "/metrics")
	viper.SetDefault("metrics.enable_go_stats", true)
	
	// Tracing defaults
	viper.SetDefault("tracing.enabled", false)
	viper.SetDefault("tracing.service_name", "ratelimiter")
	viper.SetDefault("tracing.endpoint", "localhost:4317")
	viper.SetDefault("tracing.sampling_rate", 0.1)
	
	// Logging defaults
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "json")
	viper.SetDefault("logging.output", "stdout")
	viper.SetDefault("logging.time_format", time.RFC3339)
	viper.SetDefault("logging.enable_caller", true)
}

// bindEnvVariables binds specific environment variables to configuration keys
func bindEnvVariables() {
	// Bind sensitive variables
	_ = viper.BindEnv("redis.password", "RATELIMITER_REDIS_PASSWORD")
	_ = viper.BindEnv("auth.jwt_secret", "RATELIMITER_JWT_SECRET")
	_ = viper.BindEnv("auth.basic_auth.username", "RATELIMITER_BASIC_AUTH_USERNAME")
	_ = viper.BindEnv("auth.basic_auth.password", "RATELIMITER_BASIC_AUTH_PASSWORD")
	
	// Bind other common variables
	_ = viper.BindEnv("http.address", "RATELIMITER_HTTP_ADDRESS")
	_ = viper.BindEnv("grpc.address", "RATELIMITER_GRPC_ADDRESS")
	_ = viper.BindEnv("redis.addresses", "RATELIMITER_REDIS_ADDRESSES")
	_ = viper.BindEnv("logging.level", "RATELIMITER_LOG_LEVEL")
}

// loadSecretsFromEnv loads sensitive configuration from environment variables
func loadSecretsFromEnv(config *Config) {
	// Redis password
	if redisPassword := os.Getenv("RATELIMITER_REDIS_PASSWORD"); redisPassword != "" {
		config.Redis.Password = redisPassword
	}
	
	// JWT secret
	if jwtSecret := os.Getenv("RATELIMITER_JWT_SECRET"); jwtSecret != "" {
		config.Auth.JWTSecret = jwtSecret
	}
	
	// Basic auth credentials
	if username := os.Getenv("RATELIMITER_BASIC_AUTH_USERNAME"); username != "" {
		config.Auth.BasicAuth.Username = username
	}
	
	if password := os.Getenv("RATELIMITER_BASIC_AUTH_PASSWORD"); password != "" {
		config.Auth.BasicAuth.Password = password
	}
	
	// API keys from environment (comma separated)
	if apiKeys := os.Getenv("RATELIMITER_API_KEYS"); apiKeys != "" {
		keys := strings.Split(apiKeys, ",")
		for i, key := range keys {
			parts := strings.Split(strings.TrimSpace(key), ":")
			if len(parts) >= 2 {
				apiKey := APIKeyConfig{
					Key:      parts[0],
					Role:     parts[1],
					IsActive: true,
				}
				config.Auth.APIKeys = append(config.Auth.APIKeys, apiKey)
			}
		}
	}
} 
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Server
	HTTPPort    string
	WSPort      string
	Environment string

	// PostgreSQL
	DatabaseURL      string
	DBMaxOpenConns   int
	DBMaxIdleConns   int
	DBConnMaxLifetime time.Duration

	// Redis
	RedisURL      string
	RedisPassword string
	RedisPoolSize int

	// NATS
	NatsURL string

	// ClickHouse
	ClickHouseURL string

	// JWT
	JWTSecret          string
	AccessTokenExpiry  time.Duration
	RefreshTokenExpiry time.Duration

	// ED25519 signing keys (hex-encoded)
	ED25519PrivateKeyHex string
	ED25519PublicKeyHex  string

	// Odds Provider
	OddsProvider       string
	EntitySportsAPIKey string
	EntitySportsURL    string
	MockVolatility     float64
	MockUpdateInterval time.Duration

	// Rate Limiting
	RateLimitRPS   int
	RateLimitBurst int

	// Cloudflare
	CloudflareToken string

	// Security
	CORSOrigins    []string
	MaxBodySizeMB  int
	TrustedProxies []string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	env := getEnv("ENVIRONMENT", "development")

	corsDefault := []string{"http://localhost:3000"}
	if env == "production" {
		corsDefault = []string{}
	}

	return &Config{
		HTTPPort:    getEnv("HTTP_PORT", "8080"),
		WSPort:      getEnv("WS_PORT", "8081"),
		Environment: env,

		DatabaseURL:       getEnv("DATABASE_URL", ""),
		DBMaxOpenConns:    getIntEnv("DB_MAX_OPEN_CONNS", 25),
		DBMaxIdleConns:    getIntEnv("DB_MAX_IDLE_CONNS", 10),
		DBConnMaxLifetime: getDurationEnv("DB_CONN_MAX_LIFETIME", 5*time.Minute),

		RedisURL:      getEnv("REDIS_URL", "localhost:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisPoolSize: getIntEnv("REDIS_POOL_SIZE", 10),

		NatsURL: getEnv("NATS_URL", "nats://localhost:4222"),

		ClickHouseURL: getEnv("CLICKHOUSE_URL", "tcp://localhost:9000"),

		JWTSecret:          getEnv("JWT_SECRET", "change-me-in-production"),
		AccessTokenExpiry:  getDurationEnv("ACCESS_TOKEN_EXPIRY", 15*time.Minute),
		RefreshTokenExpiry: getDurationEnv("REFRESH_TOKEN_EXPIRY", 7*24*time.Hour),

		ED25519PrivateKeyHex: getEnv("ED25519_PRIVATE_KEY", ""),
		ED25519PublicKeyHex:  getEnv("ED25519_PUBLIC_KEY", ""),

		OddsProvider:       getEnv("ODDS_PROVIDER", "mock"),
		EntitySportsAPIKey: getEnv("ENTITY_SPORTS_API_KEY", ""),
		EntitySportsURL:    getEnv("ENTITY_SPORTS_URL", "https://rest.entitysport.com/v2"),
		MockVolatility:     getFloatEnv("MOCK_VOLATILITY", 0.05),
		MockUpdateInterval: getDurationEnv("MOCK_UPDATE_INTERVAL", 2*time.Second),

		RateLimitRPS:   getIntEnv("RATE_LIMIT_RPS", 100),
		RateLimitBurst: getIntEnv("RATE_LIMIT_BURST", 200),

		CloudflareToken: getEnv("CLOUDFLARE_TOKEN", ""),

		CORSOrigins:    getStringSliceEnv("CORS_ORIGINS", corsDefault),
		MaxBodySizeMB:  getIntEnv("MAX_BODY_SIZE_MB", 1),
		TrustedProxies: getStringSliceEnv("TRUSTED_PROXIES", []string{}),
	}
}

// Validate checks that the configuration is safe for the target environment.
// It returns an error if any critical security requirements are not met.
func (c *Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("config: DATABASE_URL is required")
	}

	if c.Environment == "production" {
		if c.JWTSecret == "change-me-in-production" || c.JWTSecret == "" {
			return fmt.Errorf("config: JWT_SECRET must be set to a secure value in production")
		}
		if len(c.JWTSecret) < 32 {
			return fmt.Errorf("config: JWT_SECRET must be at least 32 characters in production")
		}

		for _, origin := range c.CORSOrigins {
			if origin == "*" {
				return fmt.Errorf("config: CORS_ORIGINS must not contain wildcard '*' in production")
			}
		}

		if len(c.ED25519PrivateKeyHex) == 0 || len(c.ED25519PublicKeyHex) == 0 {
			return fmt.Errorf("config: ED25519_PRIVATE_KEY and ED25519_PUBLIC_KEY must be set in production")
		}
	}

	if c.DBMaxOpenConns < 1 {
		return fmt.Errorf("config: DB_MAX_OPEN_CONNS must be at least 1")
	}
	if c.DBMaxIdleConns < 1 {
		return fmt.Errorf("config: DB_MAX_IDLE_CONNS must be at least 1")
	}
	if c.RedisPoolSize < 1 {
		return fmt.Errorf("config: REDIS_POOL_SIZE must be at least 1")
	}

	return nil
}

// IsProduction returns true if the environment is set to "production".
func (c *Config) IsProduction() bool {
	return c.Environment == "production"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getFloatEnv(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getStringSliceEnv(key string, fallback []string) []string {
	if v := os.Getenv(key); v != "" {
		parts := strings.Split(v, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

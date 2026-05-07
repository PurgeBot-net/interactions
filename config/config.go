package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	// Discord
	Token         string `env:"DISCORD_TOKEN,required"`
	PublicKey     string `env:"DISCORD_PUBLIC_KEY,required"`
	ApplicationID uint64 `env:"DISCORD_APPLICATION_ID,required"`

	// Database
	DatabaseHost     string `env:"DATABASE_HOST"     envDefault:"localhost"`
	DatabasePort     int    `env:"DATABASE_PORT"     envDefault:"5432"`
	DatabaseName     string `env:"DATABASE_NAME"     envDefault:"purgebot"`
	DatabaseUser     string `env:"DATABASE_USER"     envDefault:"purgebot"`
	DatabasePassword string `env:"DATABASE_PASSWORD"`

	// Redis
	RedisAddr     string `env:"REDIS_ADDR"     envDefault:"localhost:6379"`
	RedisPassword string `env:"REDIS_PASSWORD"`
	RedisDB       int    `env:"REDIS_DB"       envDefault:"0"`

	// Server
	Addr string `env:"INTERACTIONS_ADDR" envDefault:":8080"`

	// Observability
	SentryDSN string `env:"SENTRY_DSN"`
	LogLevel  string `env:"LOG_LEVEL"  envDefault:"info"`
	LogJSON   bool   `env:"LOG_JSON"`

	// Premium
	PremiumSKUID        string   `env:"PREMIUM_SKU_ID"`
	FreePremiumGuildIDs []uint64 `env:"FREE_PREMIUM_GUILD_IDS" envSeparator:","`
}

func Load() (Config, error) {
	return env.ParseAs[Config]()
}

func (c *Config) DatabaseURL() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		c.DatabaseUser, c.DatabasePassword, c.DatabaseHost, c.DatabasePort, c.DatabaseName)
}

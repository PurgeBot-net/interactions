package config

import (
	"github.com/caarlos0/env/v11"

	"github.com/PurgeBot-net/common/log"
	"github.com/PurgeBot-net/common/rdb"
	"github.com/PurgeBot-net/database"
)

type Config struct {
	// Discord
	Token         string `env:"DISCORD_TOKEN,required"`
	PublicKey     string `env:"DISCORD_PUBLIC_KEY,required"`
	ApplicationID uint64 `env:"DISCORD_APPLICATION_ID,required"`

	// Server
	Addr string `env:"INTERACTIONS_ADDR" envDefault:":8080"`

	// Premium
	PremiumSKUID        uint64   `env:"PREMIUM_SKU_ID"`
	FreePremiumGuildIDs []uint64 `env:"FREE_PREMIUM_GUILD_IDS" envSeparator:","`

	Database database.Config
	Redis    rdb.Config
	Log      log.Config
}

func Load() (Config, error) {
	return env.ParseAs[Config]()
}

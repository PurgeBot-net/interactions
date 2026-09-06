package commands

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"go.uber.org/zap"
)

const (
	guildOwnerKey = "purgebot:guild_owner:%d"
	guildOwnerTTL = 24 * time.Hour
	// respond writes the live HTTP response, so a lookup runs inside Discord's 3s window.
	guildOwnerLookupTimeout = 1500 * time.Millisecond
)

// The gateway seeds this key from guild events; the REST fetch is the fallback for when
// it is down or Redis lost the key.
func (r *Router) guildOwnerID(ctx context.Context, guildID snowflake.ID) uint64 {
	cacheKey := fmt.Sprintf(guildOwnerKey, uint64(guildID))
	if val, err := r.redis.Get(ctx, cacheKey).Result(); err == nil {
		// A corrupt or zero entry falls through to REST rather than denying for the whole TTL.
		if id, err := strconv.ParseUint(val, 10, 64); err == nil && id != 0 {
			return id
		}
	}

	lookupCtx, cancel := context.WithTimeout(ctx, guildOwnerLookupTimeout)
	defer cancel()

	guild, err := r.client.Rest.GetGuild(guildID, false, rest.WithCtx(lookupCtx))
	if err != nil {
		r.logger.Warn("get guild owner", zap.Error(err), zap.String("guild_id", guildID.String()))
		return 0
	}

	r.redis.Set(ctx, cacheKey, uint64(guild.OwnerID), guildOwnerTTL) //nolint:errcheck
	return uint64(guild.OwnerID)
}

func (r *Router) canCancelJob(ctx context.Context, i discord.Interaction, requestedByID uint64) bool {
	actorID := uint64(i.User().ID)
	if actorID == requestedByID {
		return true
	}
	if i.GuildID() == nil {
		return false
	}
	return canCancel(actorID, requestedByID, r.guildOwnerID(ctx, *i.GuildID()))
}

// guildOwnerID 0 means unresolved, and never matches a real snowflake.
func canCancel(actorID, requestedByID, guildOwnerID uint64) bool {
	return actorID == requestedByID || (guildOwnerID != 0 && actorID == guildOwnerID)
}

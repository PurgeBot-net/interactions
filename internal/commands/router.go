package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/PurgeBot-net/database"
	"github.com/PurgeBot-net/interactions/config"
)

type RespondFunc func(discord.InteractionResponse)

type Router struct {
	cfg    config.Config
	logger *zap.Logger
	db     *database.Database
	redis  *redis.Client
	client *bot.Client
}

func NewRouter(cfg config.Config, logger *zap.Logger, db *database.Database, redis *redis.Client, client *bot.Client) *Router {
	return &Router{cfg: cfg, logger: logger, db: db, redis: redis, client: client}
}

// RegisterCommands pushes all slash command definitions to Discord.
func (r *Router) RegisterCommands(_ context.Context) error {
	appID := snowflake.ID(r.cfg.ApplicationID)
	if _, err := r.client.Rest.SetGlobalCommands(appID, GlobalCommands()); err != nil {
		return err
	}
	r.logger.Info("registered slash commands")
	return nil
}

// Handle routes an incoming interaction to the correct handler.
func (r *Router) Handle(ctx context.Context, interaction discord.Interaction, respond RespondFunc) {
	switch i := interaction.(type) {
	case discord.ApplicationCommandInteraction:
		r.handleCommand(ctx, i, respond)
	case discord.AutocompleteInteraction:
		r.handleAutocomplete(ctx, i, respond)
	case discord.ModalSubmitInteraction:
		r.handleModal(ctx, i, respond)
	case discord.ComponentInteraction:
		r.handleComponent(ctx, i, respond)
	}
}

func (r *Router) handleCommand(ctx context.Context, i discord.ApplicationCommandInteraction, respond RespondFunc) {
	switch i.Data.CommandName() {
	case "purge":
		newPurgeHandler(r).Handle(ctx, i, respond)
	case "help":
		newHelpHandler(r).Handle(ctx, i, respond)
	case "customize":
		newCustomizeHandler(r).Handle(ctx, i, respond)
	case "stats":
		newStatsHandler(r).Handle(ctx, i, respond)
	default:
		r.logger.Warn("unknown command", zap.String("name", i.Data.CommandName()))
	}
}

const premiumCacheTTL = 2 * time.Minute

func (r *Router) hasPremium(ctx context.Context, guildID snowflake.ID) bool {
	for _, id := range r.cfg.FreePremiumGuildIDs {
		if id == uint64(guildID) {
			return true
		}
	}
	if r.cfg.PremiumSKUID == "" {
		return false
	}

	cacheKey := fmt.Sprintf("purgebot:premium:%d", guildID)
	if val, err := r.redis.Get(ctx, cacheKey).Result(); err == nil {
		return val == "1"
	}

	skuID, err := snowflake.Parse(r.cfg.PremiumSKUID)
	if err != nil {
		return false
	}
	entitlements, err := r.client.Rest.GetEntitlements(
		snowflake.ID(r.cfg.ApplicationID),
		rest.GetEntitlementsParams{
			GuildID:      guildID,
			SkuIDs:       []snowflake.ID{skuID},
			ExcludeEnded: true,
		},
	)
	if err != nil {
		r.logger.Warn("check entitlements", zap.Error(err))
		return false
	}

	result := len(entitlements) > 0
	cacheVal := "0"
	if result {
		cacheVal = "1"
	}
	r.redis.Set(ctx, cacheKey, cacheVal, premiumCacheTTL) //nolint:errcheck
	return result
}

func (r *Router) handleAutocomplete(ctx context.Context, i discord.AutocompleteInteraction, respond RespondFunc) {
	if i.Data.Focused().Name == "target_id" {
		newPurgeHandler(r).HandleAutocomplete(ctx, i, respond)
		return
	}
	respond(discord.InteractionResponse{
		Type: discord.InteractionResponseTypeAutocompleteResult,
		Data: discord.AutocompleteResult{},
	})
}

func (r *Router) handleModal(ctx context.Context, i discord.ModalSubmitInteraction, respond RespondFunc) {
	switch i.Data.CustomID {
	case "customize_modal":
		newCustomizeHandler(r).HandleModal(ctx, i, respond)
	}
}

func (r *Router) handleComponent(ctx context.Context, i discord.ComponentInteraction, respond RespondFunc) {
	id := i.Data.CustomID()
	switch {
	case strings.HasPrefix(id, "cancel:"):
		newPurgeHandler(r).HandleCancel(ctx, i, respond)
	case strings.HasPrefix(id, "skip:"):
		newPurgeHandler(r).HandleSkip(ctx, i, respond)
	}
}

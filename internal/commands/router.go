package commands

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/PurgeBot-net/common/job"
	"github.com/PurgeBot-net/database"
	"github.com/PurgeBot-net/interactions/config"
	"github.com/PurgeBot-net/locale"
)

type RespondFunc func(discord.InteractionResponse)

type Router struct {
	cfg      config.Config
	logger   *zap.Logger
	db       *database.Database
	redis    *redis.Client
	client   *bot.Client
	inflight sync.WaitGroup
}

func NewRouter(cfg config.Config, logger *zap.Logger, db *database.Database, redis *redis.Client, client *bot.Client) *Router {
	return &Router{cfg: cfg, logger: logger, db: db, redis: redis, client: client}
}

// RegisterCommands pushes all slash command definitions to Discord.
func (r *Router) RegisterCommands(ctx context.Context) error {
	appID := snowflake.ID(r.cfg.ApplicationID)
	if _, err := r.client.Rest.SetGlobalCommands(appID, GlobalCommands(), rest.WithCtx(ctx)); err != nil {
		return err
	}
	r.logger.Info("registered slash commands")
	return nil
}

// Handle routes an incoming interaction to the correct handler.
func (r *Router) Handle(ctx context.Context, interaction discord.Interaction, respond RespondFunc) {
	switch i := interaction.(type) {
	case discord.ApplicationCommandInteraction:
		// Opens a modal, which Discord accepts only as the first response. Gates itself.
		if commandPath(i) == "/customize/edit" {
			newCustomizeHandler(r).HandleEdit(ctx, i, respond)
			return
		}
		r.deferred(ctx, i, respond, deferEphemeral(i), func(ctx context.Context, respond RespondFunc) {
			r.handleCommand(ctx, i, respond)
		})
	case discord.AutocompleteInteraction:
		r.handleAutocomplete(ctx, i, respond)
	case discord.ModalSubmitInteraction:
		r.deferred(ctx, i, respond, false, func(ctx context.Context, respond RespondFunc) {
			r.handleModal(ctx, i, respond)
		})
	case discord.ComponentInteraction:
		// Not deferred: already bounded under 3s, and an early ack lets a second skip:continue
		// click overwrite "Starting" with "expired".
		r.handleComponent(ctx, i, respond)
	}
}

func (r *Router) Wait() { r.inflight.Wait() }

func (r *Router) handleCommand(ctx context.Context, i discord.ApplicationCommandInteraction, respond RespondFunc) {
	if !r.passGate(ctx, i, commandGate(commandPath(i)), respond) {
		return
	}

	switch i.Data.CommandName() {
	case "purge":
		newPurgeHandler(r).Handle(ctx, i, respond)
	case "cancel":
		newCancelHandler(r).Handle(ctx, i, respond)
	case "help":
		newHelpHandler(r).Handle(ctx, i, respond)
	case "customize":
		newCustomizeHandler(r).Handle(ctx, i, respond)
	case "stats":
		newStatsHandler(r).Handle(ctx, i, respond)
	case "settings":
		newSettingsHandler(r).Handle(ctx, i, respond)
	default:
		r.logger.Warn("unknown command", zap.String("name", i.Data.CommandName()))
	}
}

const premiumCacheTTL = 2 * time.Minute

// A failed lookup is an error, never a "no": the upsell would tell a paying guild to buy Premium.
func (r *Router) hasPremium(ctx context.Context, guildID snowflake.ID) (bool, error) {
	if slices.Contains(r.cfg.FreePremiumGuildIDs, uint64(guildID)) {
		return true, nil
	}
	if r.cfg.PremiumSKUID == 0 {
		return false, nil
	}

	cacheKey := fmt.Sprintf("purgebot:premium:%d", guildID)
	if val, err := r.redis.Get(ctx, cacheKey).Result(); err == nil {
		return val == "1", nil
	}

	entitlements, err := r.client.Rest.GetEntitlements(
		snowflake.ID(r.cfg.ApplicationID),
		rest.GetEntitlementsParams{
			GuildID:      guildID,
			SkuIDs:       []snowflake.ID{snowflake.ID(r.cfg.PremiumSKUID)},
			ExcludeEnded: true,
		},
		rest.WithCtx(ctx),
	)
	if err != nil {
		r.logger.Warn("check entitlements", zap.Error(err))
		return false, err
	}

	result := len(entitlements) > 0
	cacheVal := "0"
	if result {
		cacheVal = "1"
	}
	r.redis.Set(ctx, cacheKey, cacheVal, premiumCacheTTL) //nolint:errcheck
	return result, nil
}

func (r *Router) premiumUpsell(i discord.Interaction, msg locale.Message) discord.InteractionResponse {
	m := discord.NewMessageCreateV2(discord.NewContainer(discord.NewTextDisplay(msg.In(interactionLocale(i))))).WithEphemeral(true)
	if r.cfg.PremiumSKUID != 0 {
		m.Components = append(m.Components, discord.NewActionRow(discord.NewPremiumButton(snowflake.ID(r.cfg.PremiumSKUID))))
	}
	return discord.InteractionResponse{Type: discord.InteractionResponseTypeCreateMessage, Data: m}
}

func (r *Router) handleAutocomplete(ctx context.Context, i discord.AutocompleteInteraction, respond RespondFunc) {
	if i.Data.Focused().Name == "target" {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if ok, err := r.checkAccess(ctx, i, commandGate(i.Data.CommandPath())); !ok {
			if err != nil {
				r.logger.Warn("check access for autocomplete", zap.Error(err))
			}
			respond(autocompleteResult(nil))
			return
		}
		newPurgeHandler(r).HandleAutocomplete(ctx, i, respond)
		return
	}
	respond(autocompleteResult(nil))
}

func (r *Router) handleModal(ctx context.Context, i discord.ModalSubmitInteraction, respond RespondFunc) {
	switch i.Data.CustomID {
	case "customize_modal":
		if !r.passGate(ctx, i, gateGranted, respond) {
			return
		}
		newCustomizeHandler(r).HandleModal(ctx, i, respond)
	}
}

func (r *Router) handleComponent(ctx context.Context, i discord.ComponentInteraction, respond RespondFunc) {
	id := i.Data.CustomID()
	switch {
	case strings.HasPrefix(id, job.CancelButtonPrefix):
		newPurgeHandler(r).HandleCancel(ctx, i, respond)
	case strings.HasPrefix(id, "skip:"):
		newPurgeHandler(r).HandleSkip(ctx, i, respond)
	default:
		respond(discord.InteractionResponse{Type: discord.InteractionResponseTypeDeferredUpdateMessage})
	}
}

package commands

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"go.uber.org/zap"

	"github.com/PurgeBot-net/locale"
)

type settingsHandler struct{ r *Router }

func newSettingsHandler(r *Router) *settingsHandler { return &settingsHandler{r} }

// No Premium gate: every server's purges are recorded, so every server can turn that off.
func (h *settingsHandler) Handle(ctx context.Context, i discord.ApplicationCommandInteraction, respond RespondFunc) {
	if i.GuildID() == nil {
		respond(ephemeralLocale(i, locale.MsgErrorGuildOnly))
		return
	}
	if i.Member() == nil || !i.Member().Permissions.Has(discord.PermissionAdministrator) {
		respond(ephemeralLocale(i, locale.MsgSettingsNoPerms))
		return
	}

	guildID := int64(*i.GuildID())
	if i.SlashCommandInteractionData().String("stats") == "off" {
		if err := h.r.db.DisableStats(ctx, guildID); err != nil {
			h.r.logger.Error("disable stats", zap.Error(err))
			respond(ephemeralLocale(i, locale.MsgErrorInternal))
			return
		}
		respond(publicLocale(i, locale.MsgSettingsStatsOff))
		return
	}

	if err := h.r.db.EnableStats(ctx, guildID); err != nil {
		h.r.logger.Error("enable stats", zap.Error(err))
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}
	respond(publicLocale(i, locale.MsgSettingsStatsOn))
}

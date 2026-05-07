package commands

import (
	"context"
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"go.uber.org/zap"

	"github.com/PurgeBot-net/locale"
)

type statsHandler struct{ r *Router }

func newStatsHandler(r *Router) *statsHandler { return &statsHandler{r} }

func (h *statsHandler) Handle(ctx context.Context, i discord.ApplicationCommandInteraction, respond RespondFunc) {
	if i.GuildID() == nil {
		respond(ephemeralLocale(i, locale.MsgErrorGuildOnly))
		return
	}

	if !h.r.hasPremium(ctx, *i.GuildID()) {
		components := []discord.LayoutComponent{
			discord.NewContainer(
				discord.NewTextDisplay(locale.MsgStatsNoPremium.In(interactionLocale(i))),
			),
		}
		if h.r.cfg.PremiumSKUID != "" {
			if skuID, err := snowflake.Parse(h.r.cfg.PremiumSKUID); err == nil {
				components = append(components, discord.ActionRowComponent{
					Components: []discord.InteractiveComponent{
						discord.NewPremiumButton(skuID),
					},
				})
			}
		}
		respond(discord.InteractionResponse{
			Type: discord.InteractionResponseTypeCreateMessage,
			Data: discord.MessageCreate{
				Flags:      discord.MessageFlagIsComponentsV2 | discord.MessageFlagEphemeral,
				Components: components,
			},
		})
		return
	}

	lang := interactionLocale(i)

	stats, err := h.r.db.GetGuildStats(ctx, int64(*i.GuildID()))
	if err != nil {
		h.r.logger.Error("get guild stats", zap.Error(err))
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}

	texts := []discord.ContainerSubComponent{
		discord.NewTextDisplay(locale.MsgStatsHeader.In(lang)),
	}

	if stats.TotalPurges == 0 {
		texts = append(texts, discord.NewTextDisplay(locale.MsgStatsNoPurges.In(lang)))
	} else {
		texts = append(texts, discord.NewTextDisplay(locale.MsgStatsTotals.In(lang, stats.TotalPurges, stats.TotalDeleted)))
		if stats.LastPurgeAt != nil {
			texts = append(texts, discord.NewTextDisplay(locale.MsgStatsLastPurge.In(lang, fmt.Sprintf("<t:%d:R>", stats.LastPurgeAt.Unix()))))
		}
	}

	respond(discord.InteractionResponse{
		Type: discord.InteractionResponseTypeCreateMessage,
		Data: discord.MessageCreate{
			Flags: discord.MessageFlagIsComponentsV2 | discord.MessageFlagEphemeral,
			Components: []discord.LayoutComponent{
				discord.NewContainer(texts...),
			},
		},
	})
}

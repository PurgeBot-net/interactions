package commands

import (
	"context"
	"fmt"

	"github.com/disgoorg/disgo/discord"

	"github.com/PurgeBot-net/locale"
)

type helpHandler struct{ r *Router }

func newHelpHandler(r *Router) *helpHandler { return &helpHandler{r} }

func (h *helpHandler) Handle(ctx context.Context, i discord.ApplicationCommandInteraction, respond RespondFunc) {
	lang := interactionLocale(i)

	components := []discord.LayoutComponent{
		discord.NewTextDisplay(locale.MsgHelpHeader.In(lang)),
		discord.NewSmallSeparator(),
		discord.NewContainer(
			discord.NewTextDisplay(locale.MsgHelpCommandsTitle.In(lang)),
			discord.NewTextDisplay(locale.MsgHelpCommandsBody.In(lang)),
		),
		discord.NewContainer(
			discord.NewTextDisplay(locale.MsgHelpParametersTitle.In(lang)),
			discord.NewTextDisplay(locale.MsgHelpParametersBody.In(lang)),
		),
		discord.NewContainer(
			discord.NewTextDisplay(locale.MsgHelpFilteringTitle.In(lang)),
			discord.NewTextDisplay(locale.MsgHelpFilteringBody.In(lang)),
		),
		discord.NewContainer(
			discord.NewTextDisplay(locale.MsgHelpPermissionsTitle.In(lang)),
			discord.NewTextDisplay(locale.MsgHelpPermissionsBody.In(lang)),
		),
		discord.ActionRowComponent{Components: []discord.InteractiveComponent{
			discord.ButtonComponent{
				Style: discord.ButtonStyleLink,
				Label: locale.MsgHelpButtonInvite.In(lang),
				Emoji: &discord.ComponentEmoji{Name: "🔗"},
				URL:   "https://discord.com/oauth2/authorize?client_id=" + fmt.Sprintf("%d", h.r.cfg.ApplicationID) + "&permissions=74752&integration_type=0&scope=bot",
			},
			discord.ButtonComponent{
				Style: discord.ButtonStyleLink,
				Label: locale.MsgHelpButtonSupport.In(lang),
				Emoji: &discord.ComponentEmoji{Name: "❓"},
				URL:   "https://support.purgebot.net",
			},
		}},
	}

	showBranding := true
	if i.GuildID() != nil {
		if c, err := h.r.db.GetCustomization(ctx, int64(*i.GuildID())); err == nil && c != nil {
			showBranding = !c.RemoveBranding
		}
	}
	if showBranding {
		components = append(components, discord.NewContainer(
			discord.NewTextDisplay("-# Powered by PurgeBot"),
		))
	}

	respond(discord.InteractionResponse{
		Type: discord.InteractionResponseTypeCreateMessage,
		Data: discord.MessageCreate{
			Flags:      discord.MessageFlagIsComponentsV2,
			Components: components,
		},
	})
}

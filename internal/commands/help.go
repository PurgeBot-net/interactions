package commands

import (
	"context"
	"fmt"

	"github.com/disgoorg/disgo/discord"

	"github.com/PurgeBot-net/locale"
)

type helpHandler struct{ r *Router }

func newHelpHandler(r *Router) *helpHandler { return &helpHandler{r} }

// Scopes and permissions come from Default Install Settings in the portal. Adding scope,
// integration_type or redirect_uri would silently take the URL back to overriding them.
func inviteURL(applicationID uint64) string {
	return fmt.Sprintf("https://discord.com/oauth2/authorize?client_id=%d", applicationID)
}

func (h *helpHandler) Handle(ctx context.Context, i discord.ApplicationCommandInteraction, respond RespondFunc) {
	lang := interactionLocale(i)

	card := []discord.ContainerSubComponent{discord.NewTextDisplay(locale.MsgHelpHeader.In(lang))}
	for _, section := range [][2]locale.Message{
		{locale.MsgHelpCommandsTitle, locale.MsgHelpCommandsBody},
		{locale.MsgHelpParametersTitle, locale.MsgHelpParametersBody},
		{locale.MsgHelpFilteringTitle, locale.MsgHelpFilteringBody},
		{locale.MsgHelpPermissionsTitle, locale.MsgHelpPermissionsBody},
	} {
		card = append(card,
			discord.NewSmallSeparator(),
			discord.NewTextDisplay(section[0].In(lang)+"\n"+section[1].In(lang)),
		)
	}

	components := []discord.LayoutComponent{
		discord.NewContainer(card...),
		discord.ActionRowComponent{Components: []discord.InteractiveComponent{
			discord.ButtonComponent{
				Style: discord.ButtonStyleLink,
				Label: locale.MsgHelpButtonInvite.In(lang),
				URL:   inviteURL(h.r.cfg.ApplicationID),
			},
			discord.ButtonComponent{
				Style: discord.ButtonStyleLink,
				Label: locale.MsgHelpButtonSupport.In(lang),
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
		components = append(components, discord.NewTextDisplay("-# Powered by PurgeBot"))
	}

	respond(discord.InteractionResponse{
		Type: discord.InteractionResponseTypeCreateMessage,
		Data: discord.NewMessageCreateV2(components...),
	})
}

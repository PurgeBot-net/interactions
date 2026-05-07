package commands

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/omit"
	"github.com/disgoorg/snowflake/v2"
	"go.uber.org/zap"

	"github.com/PurgeBot-net/database"
	"github.com/PurgeBot-net/locale"
)

type customizeHandler struct{ r *Router }

func newCustomizeHandler(r *Router) *customizeHandler { return &customizeHandler{r} }

func (h *customizeHandler) Handle(ctx context.Context, i discord.ApplicationCommandInteraction, respond RespondFunc) {
	if i.GuildID() == nil {
		respond(ephemeralLocale(i, locale.MsgErrorGuildOnly))
		return
	}
	if i.Member() == nil || !i.Member().Permissions.Has(discord.PermissionAdministrator) {
		respond(ephemeralLocale(i, locale.MsgCustomizeNoPerms))
		return
	}
	if !h.r.hasPremium(ctx, *i.GuildID()) {
		lang := interactionLocale(i)
		components := []discord.LayoutComponent{
			discord.NewContainer(
				discord.NewTextDisplay(locale.MsgCustomizeNoPremium.In(lang)),
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

	data := i.SlashCommandInteractionData()
	switch *data.SubCommandName {
	case "edit":
		existing, err := h.r.db.GetCustomization(ctx, int64(*i.GuildID()))
		if err != nil {
			h.r.logger.Error("get customization", zap.Error(err))
			respond(ephemeralLocale(i, locale.MsgErrorInternal))
			return
		}

		nameInput := discord.NewShortTextInput("bot_name").
			WithPlaceholder("PurgeBot").
			WithMaxLength(32).
			WithRequired(false)
		if existing != nil && existing.BotName != nil {
			nameInput = nameInput.WithValue(*existing.BotName)
		}

		removeBranding := existing != nil && existing.RemoveBranding
		brandingInput := discord.RadioGroupComponent{
			CustomID: "remove_branding",
			Options: []discord.RadioGroupOption{
				discord.NewRadioGroupOption("no", "Keep branding").
					WithDescription(`Show the "Powered by PurgeBot" footer`).
					WithDefault(!removeBranding),
				discord.NewRadioGroupOption("yes", "Remove branding").
					WithDescription(`Hide the "Powered by PurgeBot" footer`).
					WithDefault(removeBranding),
			},
		}

		respond(discord.InteractionResponse{
			Type: discord.InteractionResponseTypeModal,
			Data: discord.NewModalCreate("customize_modal", "Customize PurgeBot",
				discord.NewLabel("Bot name (leave blank to keep current)", nameInput),
				discord.NewLabel("Upload a new avatar (JPG, PNG, GIF, WebP — max 8 MB)", discord.NewFileUpload("bot_avatar").WithRequired(false)),
				discord.NewLabel("Branding footer", brandingInput),
			),
		})

	case "clear":
		if err := h.r.db.DeleteCustomization(ctx, int64(*i.GuildID())); err != nil {
			h.r.logger.Error("delete customization", zap.Error(err))
			respond(ephemeralLocale(i, locale.MsgErrorInternal))
			return
		}
		empty := ""
		if _, err := h.r.client.Rest.UpdateCurrentMember(*i.GuildID(), discord.CurrentMemberUpdate{
			Nick:   &empty,
			Avatar: omit.NewNilPtr[discord.Icon](),
		}); err != nil {
			h.r.logger.Warn("reset bot nickname/avatar", zap.Error(err))
		}
		respond(ephemeralLocale(i, locale.MsgCustomizeCleared))
	}
}

func (h *customizeHandler) HandleModal(ctx context.Context, i discord.ModalSubmitInteraction, respond RespondFunc) {
	if i.GuildID() == nil {
		respond(ephemeralLocale(i, locale.MsgErrorGuildOnly))
		return
	}

	existing, err := h.r.db.GetCustomization(ctx, int64(*i.GuildID()))
	if err != nil {
		h.r.logger.Error("get customization", zap.Error(err))
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}

	// Bot name
	var botName *string
	if name := strings.TrimSpace(i.Data.Text("bot_name")); name != "" {
		botName = &name
	} else if existing != nil {
		botName = existing.BotName
	}

	// Remove branding (radio group)
	var removeBranding bool
	if comp, ok := i.Data.Component("remove_branding"); ok {
		if rg, ok := comp.(discord.RadioGroupComponent); ok && rg.Value != nil {
			removeBranding = *rg.Value == "yes"
		}
	} else if existing != nil {
		removeBranding = existing.RemoveBranding
	}

	// Avatar (file upload — optional)
	var newIcon *discord.Icon
	var avatarBase64 *string
	if attachments, ok := i.Data.OptAttachments("bot_avatar"); ok && len(attachments) > 0 {
		att := attachments[0]
		ct := ""
		if att.ContentType != nil {
			ct = strings.Split(*att.ContentType, ";")[0]
		}
		valid := ct == "image/jpeg" || ct == "image/jpg" || ct == "image/png" || ct == "image/gif" || ct == "image/webp"
		if !valid {
			respond(ephemeral(fmt.Sprintf("Invalid avatar type: `%s`. Allowed: JPG, PNG, GIF, WebP.", ct)))
			return
		}
		if att.Size > 8*1024*1024 {
			respond(ephemeral("Avatar exceeds the 8 MB size limit."))
			return
		}
		icon, b64, dlErr := h.downloadAvatar(att.URL)
		if dlErr != nil {
			h.r.logger.Warn("download avatar", zap.Error(dlErr))
			respond(ephemeralLocale(i, locale.MsgErrorInternal))
			return
		}
		newIcon = icon
		avatarBase64 = b64
	} else if existing != nil {
		// Preserve existing stored avatar (don't re-upload, just keep DB value).
		avatarBase64 = existing.BotAvatar
	}

	p := database.UpsertCustomizationParams{
		GuildID:        int64(*i.GuildID()),
		BotName:        botName,
		BotAvatar:      avatarBase64,
		RemoveBranding: removeBranding,
		UpdatedBy:      int64(i.User().ID),
	}
	if err := h.r.db.UpsertCustomization(ctx, p); err != nil {
		h.r.logger.Error("upsert customization", zap.Error(err))
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}

	memberUpdate := discord.CurrentMemberUpdate{}
	if botName != nil {
		memberUpdate.Nick = botName
	}
	if newIcon != nil {
		memberUpdate.Avatar = omit.New(newIcon)
	}
	if memberUpdate.Nick != nil || newIcon != nil {
		if _, err := h.r.client.Rest.UpdateCurrentMember(*i.GuildID(), memberUpdate); err != nil {
			h.r.logger.Warn("apply bot nick/avatar", zap.Error(err))
		}
	}

	respond(ephemeralLocale(i, locale.MsgCustomizeSaved))
}

// downloadAvatar fetches a URL and returns a discord.Icon and its base64 representation.
func (h *customizeHandler) downloadAvatar(url string) (*discord.Icon, *string, error) {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	icon, err := discord.ParseIconRaw(data)
	if err != nil {
		return nil, nil, err
	}

	b64 := base64.StdEncoding.EncodeToString(data)
	return icon, &b64, nil
}

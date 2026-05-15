package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"go.uber.org/zap"

	"github.com/PurgeBot-net/common/job"
	"github.com/PurgeBot-net/locale"
)

type purgeHandler struct{ r *Router }

func newPurgeHandler(r *Router) *purgeHandler { return &purgeHandler{r} }

func (h *purgeHandler) Handle(ctx context.Context, i discord.ApplicationCommandInteraction, respond RespondFunc) {
	if i.GuildID() == nil {
		respond(ephemeralLocale(i, locale.MsgErrorGuildOnly))
		return
	}

	if i.Member() == nil || !i.Member().Permissions.Has(discord.PermissionManageMessages) {
		respond(ephemeralLocale(i, locale.MsgPurgeNoPerms))
		return
	}

	data := i.SlashCommandInteractionData()

	if data.SubCommandName == nil {
		respond(ephemeralLocale(i, locale.MsgErrorUnknownSubcommand))
		return
	}

	purgeType, ok := map[string]job.PurgeType{
		"user":     job.PurgeTypeUser,
		"role":     job.PurgeTypeRole,
		"everyone": job.PurgeTypeEveryone,
		"inactive": job.PurgeTypeInactive,
		"webhook":  job.PurgeTypeWebhook,
		"deleted":  job.PurgeTypeDeleted,
	}[*data.SubCommandName]
	if !ok {
		respond(ephemeralLocale(i, locale.MsgErrorUnknownSubcommand))
		return
	}

	lang := interactionLocale(i)

	j := &job.PurgeJob{
		ID:               fmt.Sprintf("%d-%d", *i.GuildID(), time.Now().UnixNano()),
		GuildID:          uint64(*i.GuildID()),
		Locale:           lang,
		PurgeType:        purgeType,
		ApplicationID:    uint64(i.ApplicationID()),
		InteractionToken: i.Token(),
		RequestedByID:    uint64(i.User().ID),
		CreatedAt:        time.Now(),
	}

	if targetStr, ok := data.OptString("target_id"); ok {
		if kind, rawID, found := strings.Cut(targetStr, ":"); found {
			if id, err := strconv.ParseUint(rawID, 10, 64); err == nil {
				j.TargetID = id
				j.TargetType = job.TargetType(kind)
			}
		}
	}
	if j.TargetType == "" {
		respond(ephemeralLocale(i, locale.MsgPurgeInvalidTarget))
		return
	}

	if days, ok := data.OptInt("days"); ok {
		j.Days = days
	}
	if filter, ok := data.OptString("filter"); ok {
		j.Filter = filter
	}
	if mode, ok := data.OptString("filter_mode"); ok {
		j.FilterMode = job.FilterMode(mode)
	}
	if cs, ok := data.OptBool("case_sensitive"); ok {
		j.CaseSensitive = cs
	}
	if it, ok := data.OptBool("include_threads"); ok {
		j.IncludeThreads = it
	}
	if ib, ok := data.OptBool("include_bots"); ok {
		j.IncludeBots = ib
	}

	switch purgeType {
	case job.PurgeTypeUser:
		if user, ok := data.OptUser("user"); ok {
			j.FilterUserID = uint64(user.ID)
		}
	case job.PurgeTypeRole:
		if role, ok := data.OptRole("role"); ok {
			j.FilterRoleID = uint64(role.ID)
		}
	}

	// Interactive channel-skip flow (category targets only).
	if skipChannels, ok := data.OptBool("skip_channels"); ok && skipChannels && j.TargetType == job.TargetTypeCategory {
		h.handleSkipChannelsUI(ctx, i, j, respond)
		return
	}

	h.enqueue(ctx, i, j, lang, respond)
}

// handleSkipChannelsUI shows an interactive channel-selection UI and stores the pending job.
func (h *purgeHandler) handleSkipChannelsUI(ctx context.Context, i discord.ApplicationCommandInteraction, j *job.PurgeJob, respond RespondFunc) {
	lang := interactionLocale(i)

	channels, err := h.r.client.Rest.GetGuildChannels(*i.GuildID())
	if err != nil {
		h.r.logger.Error("get guild channels for skip ui", zap.Error(err))
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}

	var options []discord.StringSelectMenuOption
	for _, ch := range channels {
		if ch.ParentID() == nil || uint64(*ch.ParentID()) != j.TargetID {
			continue
		}
		switch ch.Type() {
		case discord.ChannelTypeGuildText, discord.ChannelTypeGuildNews,
			discord.ChannelTypeGuildVoice, discord.ChannelTypeGuildForum:
			options = append(options, discord.StringSelectMenuOption{
				Label: strings.TrimSpace(channelEmoji(ch.Type())) + ch.Name(),
				Value: ch.ID().String(),
			})
		}
	}

	if len(options) == 0 {
		respond(ephemeralLocale(i, locale.MsgPurgeInvalidTarget))
		return
	}
	if len(options) > 25 {
		options = options[:25]
	}

	if err := job.StorePendingJob(ctx, h.r.redis, j); err != nil {
		h.r.logger.Error("store pending job", zap.Error(err))
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}

	guildIDStr := i.GuildID().String()

	respond(discord.InteractionResponse{
		Type: discord.InteractionResponseTypeCreateMessage,
		Data: discord.MessageCreate{
			Flags: discord.MessageFlagIsComponentsV2 | discord.MessageFlagEphemeral,
			Components: []discord.LayoutComponent{
				discord.NewContainer(
					discord.NewTextDisplay(locale.MsgSkipChannelsPrompt.In(lang)),
				),
				discord.ActionRowComponent{Components: []discord.InteractiveComponent{
					discord.NewStringSelectMenu("skip:select:"+guildIDStr, "").
						WithMinValues(0).
						WithMaxValues(len(options)).
						AddOptions(options...),
				}},
				discord.ActionRowComponent{Components: []discord.InteractiveComponent{
					discord.ButtonComponent{
						Style:    discord.ButtonStylePrimary,
						Label:    locale.MsgSkipChannelsContinue.In(lang),
						CustomID: "skip:continue:" + guildIDStr,
					},
					discord.ButtonComponent{
						Style:    discord.ButtonStyleSecondary,
						Label:    locale.MsgCancelButton.In(lang),
						CustomID: "skip:cancel:" + guildIDStr,
					},
				}},
			},
		},
	})
}

// HandleSkip handles the skip:select / skip:continue / skip:cancel component interactions.
func (h *purgeHandler) HandleSkip(ctx context.Context, i discord.ComponentInteraction, respond RespondFunc) {
	// Custom ID format: "skip:{action}:{guildID}"
	parts := strings.SplitN(i.Data.CustomID(), ":", 3)
	if len(parts) < 3 {
		return
	}
	action, guildIDStr := parts[1], parts[2]

	guildID, err := strconv.ParseUint(guildIDStr, 10, 64)
	if err != nil {
		return
	}

	lang := interactionLocale(i)

	switch action {
	case "select":
		// Store the user's channel selections.
		data, ok := i.Data.(discord.StringSelectMenuInteractionData)
		if !ok {
			respond(discord.InteractionResponse{Type: discord.InteractionResponseTypeDeferredUpdateMessage})
			return
		}
		var ids []uint64
		for _, v := range data.Values {
			if id, err := strconv.ParseUint(v, 10, 64); err == nil {
				ids = append(ids, id)
			}
		}
		if err := job.StoreSkipSelection(ctx, h.r.redis, guildID, ids); err != nil {
			h.r.logger.Error("store skip selection", zap.Error(err))
		}
		respond(discord.InteractionResponse{Type: discord.InteractionResponseTypeDeferredUpdateMessage})

	case "continue":
		j, err := job.GetPendingJob(ctx, h.r.redis, guildID)
		if err != nil {
			h.r.logger.Error("get pending job", zap.Error(err))
			respond(discord.InteractionResponse{
				Type: discord.InteractionResponseTypeUpdateMessage,
				Data: discord.NewMessageUpdateV2(discord.NewContainer(
					discord.NewTextDisplay(locale.MsgErrorInternal.In(lang)),
				)),
			})
			return
		}
		if j == nil {
			respond(discord.InteractionResponse{
				Type: discord.InteractionResponseTypeUpdateMessage,
				Data: discord.NewMessageUpdateV2(discord.NewContainer(
					discord.NewTextDisplay(locale.MsgSkipChannelsExpired.In(lang)),
				)),
			})
			return
		}

		// Verify only the original requester can continue.
		if i.User().ID != snowflake.ID(j.RequestedByID) {
			respond(ephemeral(locale.MsgCancelNotAllowed.In(lang)))
			return
		}

		skipIDs, _ := job.GetSkipSelection(ctx, h.r.redis, guildID)
		j.SkipChannelIDs = skipIDs
		job.DeletePendingJob(ctx, h.r.redis, guildID)

		active, err := job.SetActiveJob(ctx, h.r.redis, j)
		if err != nil {
			h.r.logger.Error("set active job (skip continue)", zap.Error(err))
			respond(discord.InteractionResponse{
				Type: discord.InteractionResponseTypeUpdateMessage,
				Data: discord.NewMessageUpdateV2(discord.NewContainer(
					discord.NewTextDisplay(locale.MsgErrorInternal.In(lang)),
				)),
			})
			return
		}
		if !active {
			respond(discord.InteractionResponse{
				Type: discord.InteractionResponseTypeUpdateMessage,
				Data: discord.NewMessageUpdateV2(discord.NewContainer(
					discord.NewTextDisplay(locale.MsgPurgeAlreadyRunning.In(lang)),
				)),
			})
			return
		}

		// Acknowledge and start. The purge worker updates the original message via j.InteractionToken.
		respond(discord.InteractionResponse{
			Type: discord.InteractionResponseTypeUpdateMessage,
			Data: discord.NewMessageUpdateV2(discord.NewContainer(
				discord.NewTextDisplay(locale.MsgPurgeStatusStarting.In(lang)),
			)),
		})

		if err := job.Enqueue(ctx, h.r.redis, j); err != nil {
			h.r.logger.Error("enqueue job (skip continue)", zap.Error(err))
			job.DeleteActiveJob(ctx, h.r.redis, j.GuildID)
			h.r.client.Rest.UpdateInteractionResponse( //nolint:errcheck
				snowflake.ID(j.ApplicationID),
				j.InteractionToken,
				discord.NewMessageUpdateV2(discord.NewContainer(
					discord.NewTextDisplay(locale.MsgErrorInternalStart.In(lang)),
				)),
			)
		}

	case "cancel":
		j, _ := job.GetPendingJob(ctx, h.r.redis, guildID)
		if j != nil && i.User().ID != snowflake.ID(j.RequestedByID) {
			respond(ephemeral(locale.MsgCancelNotAllowed.In(lang)))
			return
		}
		job.DeletePendingJob(ctx, h.r.redis, guildID)
		respond(discord.InteractionResponse{
			Type: discord.InteractionResponseTypeUpdateMessage,
			Data: discord.NewMessageUpdateV2(discord.NewContainer(
				discord.NewTextDisplay(locale.MsgPurgeCancelledHeader.In(lang)),
			)),
		})
	}
}

// enqueue stores the active job, sends the deferred response, and pushes the job to Redis.
func (h *purgeHandler) enqueue(ctx context.Context, i discord.ApplicationCommandInteraction, j *job.PurgeJob, lang string, respond RespondFunc) {
	active, err := job.SetActiveJob(ctx, h.r.redis, j)
	if err != nil {
		h.r.logger.Error("set active job", zap.Error(err))
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}
	if !active {
		respond(ephemeralLocale(i, locale.MsgPurgeAlreadyRunning))
		return
	}

	respond(discord.InteractionResponse{
		Type: discord.InteractionResponseTypeDeferredCreateMessage,
		Data: discord.NewMessageCreate(),
	})

	if err := job.Enqueue(ctx, h.r.redis, j); err != nil {
		h.r.logger.Error("enqueue purge job", zap.Error(err))
		job.DeleteActiveJob(ctx, h.r.redis, j.GuildID)
		h.r.client.Rest.UpdateInteractionResponse( //nolint:errcheck
			snowflake.ID(j.ApplicationID),
			j.InteractionToken,
			discord.NewMessageUpdateV2(discord.NewContainer(
				discord.NewTextDisplay(locale.MsgErrorInternalStart.In(lang)),
			)),
		)
	}
}


func (h *purgeHandler) HandleAutocomplete(ctx context.Context, i discord.AutocompleteInteraction, respond RespondFunc) {
	if i.GuildID() == nil {
		respond(autocompleteResult(nil))
		return
	}

	var query string
	_ = json.Unmarshal(i.Data.Focused().Value, &query)
	query = strings.ToLower(query)

	excludeServer := i.Data.SubCommandName != nil && *i.Data.SubCommandName == "everyone"

	channels, err := h.r.client.Rest.GetGuildChannels(*i.GuildID())
	if err != nil {
		h.r.logger.Error("get guild channels for autocomplete", zap.Error(err))
		respond(autocompleteResult(nil))
		return
	}

	categoryHasText := map[string]bool{}
	for _, ch := range channels {
		if ch.Type() != discord.ChannelTypeGuildCategory && ch.ParentID() != nil {
			switch ch.Type() {
			case discord.ChannelTypeGuildText,
				discord.ChannelTypeGuildNews,
				discord.ChannelTypeGuildVoice,
				discord.ChannelTypeGuildForum:
				categoryHasText[ch.ParentID().String()] = true
			}
		}
	}

	var choices []discord.AutocompleteChoice

	if !excludeServer && matches("server", query) {
		choices = append(choices, discord.AutocompleteChoiceString{
			Name:  "📍 Server",
			Value: "server:" + i.GuildID().String(),
		})
	}

	for _, ch := range channels {
		if ch.Type() != discord.ChannelTypeGuildCategory {
			continue
		}
		if !categoryHasText[ch.ID().String()] {
			continue
		}
		if !matches(ch.Name(), query) {
			continue
		}
		choices = append(choices, discord.AutocompleteChoiceString{
			Name:  "📁 " + ch.Name(),
			Value: "category:" + ch.ID().String(),
		})
	}

	for _, ch := range channels {
		switch ch.Type() {
		case discord.ChannelTypeGuildText,
			discord.ChannelTypeGuildNews,
			discord.ChannelTypeGuildVoice,
			discord.ChannelTypeGuildForum:
		default:
			continue
		}
		if !matches(ch.Name(), query) {
			continue
		}
		choices = append(choices, discord.AutocompleteChoiceString{
			Name:  channelEmoji(ch.Type()) + ch.Name(),
			Value: "channel:" + ch.ID().String(),
		})
	}

	if len(choices) > 25 {
		choices = choices[:25]
	}
	respond(autocompleteResult(choices))
}

func (h *purgeHandler) HandleCancel(ctx context.Context, i discord.ComponentInteraction, respond RespondFunc) {
	// Custom ID format: "cancel:{jobID}:{requestedByID}"
	rest := i.Data.CustomID()[7:] // strip "cancel:"
	jobID, requesterID, _ := strings.Cut(rest, ":")

	if requesterID != i.User().ID.String() {
		respond(ephemeral(locale.MsgCancelNotAllowed.In(interactionLocale(i))))
		return
	}

	if err := job.Cancel(ctx, h.r.redis, jobID); err != nil {
		h.r.logger.Error("cancel job", zap.Error(err))
	}

	lang := interactionLocale(i)
	respond(discord.InteractionResponse{
		Type: discord.InteractionResponseTypeUpdateMessage,
		Data: discord.NewMessageUpdateV2(discord.NewContainer(
			discord.NewTextDisplay(locale.MsgCancelRequested.In(lang)),
		)),
	})
}

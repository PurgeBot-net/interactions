package commands

import (
	"context"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"go.uber.org/zap"

	"github.com/PurgeBot-net/database"
	"github.com/PurgeBot-net/locale"
)

const (
	// Keeps the list inside Discord's 2000-character message limit.
	maxListedRoles  = 50
	roleListTimeout = time.Second
)

// Empty slices, not nil: nil marshals to null, which Discord documents only for users.
var noPings = &discord.AllowedMentions{
	Parse: []discord.AllowedMentionType{},
	Roles: []snowflake.ID{},
	Users: []snowflake.ID{},
}

type settingsHandler struct{ r *Router }

func newSettingsHandler(r *Router) *settingsHandler { return &settingsHandler{r} }

// No Premium gate: every server's purges are recorded, so every server can turn that off.
func (h *settingsHandler) Handle(ctx context.Context, i discord.ApplicationCommandInteraction, respond RespondFunc) {
	if i.GuildID() == nil {
		respond(ephemeralLocale(i, locale.MsgErrorGuildOnly))
		return
	}

	switch path := i.SlashCommandInteractionData().CommandPath(); path {
	case "/settings/stats":
		h.stats(ctx, i, respond)
	case "/settings/access/add", "/settings/access/remove":
		h.changeAccess(ctx, i, path == "/settings/access/add", respond)
	case "/settings/access/list":
		h.listAccess(ctx, i, respond)
	default:
		respond(ephemeralLocale(i, locale.MsgErrorUnknownSubcommand))
	}
}

func (h *settingsHandler) stats(ctx context.Context, i discord.ApplicationCommandInteraction, respond RespondFunc) {
	guildID := int64(*i.GuildID())
	if i.SlashCommandInteractionData().String("state") == "off" {
		if err := h.r.db.DisableStats(ctx, guildID); err != nil {
			h.r.logger.Error("disable stats", zap.Error(err))
			respond(ephemeralLocale(i, locale.MsgErrorInternal))
			return
		}
		respond(containerMessage(locale.MsgSettingsStatsOff.In(interactionLocale(i))))
		return
	}

	if err := h.r.db.EnableStats(ctx, guildID); err != nil {
		h.r.logger.Error("enable stats", zap.Error(err))
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}
	respond(containerMessage(locale.MsgSettingsStatsOn.In(interactionLocale(i))))
}

func (h *settingsHandler) changeAccess(ctx context.Context, i discord.ApplicationCommandInteraction, add bool, respond RespondFunc) {
	role, ok := i.SlashCommandInteractionData().OptRole("role")
	if !ok {
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}
	// Discord leaves @everyone out of a member's roles, so a grant could never match.
	if role.ID == *i.GuildID() {
		respond(ephemeralContainer(locale.MsgSettingsAccessEveryone.In(interactionLocale(i))))
		return
	}

	lang := interactionLocale(i)
	p := database.AccessRoleParams{GuildID: int64(*i.GuildID()), RoleID: int64(role.ID)}
	if !add {
		if err := h.r.db.RemoveAccessRole(ctx, p); err != nil {
			h.r.logger.Error("remove access role", zap.Error(err))
			respond(ephemeralLocale(i, locale.MsgErrorInternal))
			return
		}
		respond(withoutPings(locale.MsgSettingsAccessRemoved.In(lang, discord.RoleMention(role.ID))))
		return
	}

	if err := h.r.db.AddAccessRole(ctx, p); err != nil {
		h.r.logger.Error("add access role", zap.Error(err))
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}
	respond(withoutPings(locale.MsgSettingsAccessAdded.In(lang, discord.RoleMention(role.ID))))
}

func (h *settingsHandler) listAccess(ctx context.Context, i discord.ApplicationCommandInteraction, respond RespondFunc) {
	guildID := *i.GuildID()
	ids, err := h.r.db.GetAccessRoles(ctx, int64(guildID))
	if err != nil {
		h.r.logger.Error("get access roles", zap.Error(err))
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}
	if len(ids) > 0 {
		ids = h.dropDeletedRoles(ctx, guildID, ids)
	}
	if len(ids) == 0 {
		respond(ephemeralContainer(locale.MsgSettingsAccessListEmpty.In(interactionLocale(i))))
		return
	}

	lines := make([]string, 0, min(len(ids), maxListedRoles)+1)
	for _, id := range ids[:min(len(ids), maxListedRoles)] {
		lines = append(lines, "• "+discord.RoleMention(snowflake.ID(id)))
	}
	if len(ids) > maxListedRoles {
		lines = append(lines, "• ...")
	}
	respond(withoutPings(locale.MsgSettingsAccessList.In(interactionLocale(i), strings.Join(lines, "\n")), discord.MessageFlagEphemeral))
}

// Catches roles deleted while the gateway was down. A failed role fetch keeps every grant.
func (h *settingsHandler) dropDeletedRoles(ctx context.Context, guildID snowflake.ID, ids []int64) []int64 {
	fetchCtx, cancel := context.WithTimeout(ctx, roleListTimeout)
	defer cancel()

	roles, err := h.r.client.Rest.GetRoles(guildID, rest.WithCtx(fetchCtx))
	if err != nil {
		h.r.logger.Warn("get roles for access list", zap.Error(err), zap.String("guild_id", guildID.String()))
		return ids
	}

	kept, gone := splitDeletedRoles(ids, roles)
	for _, id := range gone {
		if err := h.r.db.RemoveAccessRole(ctx, database.AccessRoleParams{GuildID: int64(guildID), RoleID: id}); err != nil {
			h.r.logger.Warn("remove grant of deleted role", zap.Error(err), zap.String("guild_id", guildID.String()))
		}
	}
	return kept
}

// A real guild always has @everyone, so an empty role list is treated as a bad answer and removes nothing.
func splitDeletedRoles(ids []int64, roles []discord.Role) (kept, gone []int64) {
	if len(roles) == 0 {
		return ids, nil
	}
	exists := make(map[int64]bool, len(roles))
	for _, role := range roles {
		exists[int64(role.ID)] = true
	}
	for _, id := range ids {
		if exists[id] {
			kept = append(kept, id)
		} else {
			gone = append(gone, id)
		}
	}
	return kept, gone
}

func withoutPings(text string, flags ...discord.MessageFlags) discord.InteractionResponse {
	msg := discord.MessageCreate{
		Flags:           discord.MessageFlagIsComponentsV2,
		Components:      []discord.LayoutComponent{discord.NewContainer(discord.NewTextDisplay(text))},
		AllowedMentions: noPings,
	}
	return discord.InteractionResponse{
		Type: discord.InteractionResponseTypeCreateMessage,
		Data: msg.WithFlags(flags...),
	}
}

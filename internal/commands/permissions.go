package commands

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"go.uber.org/zap"

	"github.com/PurgeBot-net/common/job"
	"github.com/PurgeBot-net/locale"
)

const (
	// Three calls share this, inside Discord's 3s window.
	purgeGateTimeout = 1200 * time.Millisecond

	// permissions=74752, the set the invite asks for (help.go).
	requiredPurgePerms = discord.PermissionViewChannel |
		discord.PermissionReadMessageHistory |
		discord.PermissionManageMessages

	maxDeficitLines = 25
)

type channelDeficit struct {
	ChannelID snowflake.ID
	Missing   discord.Permissions
}

// Fails open: refusing every purge in the guild is worse than missing a denial Discord was too
// unreachable to report.
func (r *Router) checkPurgePermissions(ctx context.Context, i discord.Interaction, j *job.PurgeJob, lang string) (string, bool) {
	// Discord resolves Administrator before overwrites, so no channel can revoke it.
	if p := i.AppPermissions(); p != nil && p.Has(discord.PermissionAdministrator) {
		return "", false
	}

	guildID := snowflake.ID(j.GuildID)
	botID := snowflake.ID(r.cfg.ApplicationID)

	gateCtx, cancel := context.WithTimeout(ctx, purgeGateTimeout)
	defer cancel()

	var (
		roles       []discord.Role
		member      *discord.Member
		channels    []discord.GuildChannel
		rolesErr    error
		memberErr   error
		channelsErr error
		wg          sync.WaitGroup
	)

	wg.Add(3)
	go func() {
		defer wg.Done()
		roles, rolesErr = r.client.Rest.GetRoles(guildID, rest.WithCtx(gateCtx))
	}()
	go func() {
		defer wg.Done()
		member, memberErr = r.client.Rest.GetMember(guildID, botID, rest.WithCtx(gateCtx))
	}()
	go func() {
		defer wg.Done()
		channels, channelsErr = r.client.Rest.GetGuildChannels(guildID, rest.WithCtx(gateCtx))
	}()
	wg.Wait()

	// A partial result computes as zero permissions everywhere, so all three are checked together.
	if rolesErr != nil || memberErr != nil || channelsErr != nil || member == nil {
		r.logger.Warn("purge permission gate",
			zap.Error(errors.Join(rolesErr, memberErr, channelsErr)),
			zap.String("guild_id", guildID.String()))
		return "", false
	}

	rolePerms := make(map[snowflake.ID]discord.Permissions, len(roles))
	for _, role := range roles {
		rolePerms[role.ID] = role.Permissions
	}
	base := botGuildPermissions(guildID, member.RoleIDs, rolePerms)

	targets := purgeTargetChannels(channels, j)
	if len(targets) == 0 {
		return "", false
	}

	var deficits []channelDeficit
	evaluated := len(targets)
	for _, ch := range targets {
		have := botChannelPermissions(base, guildID, botID, member.RoleIDs, ch.PermissionOverwrites())
		missing := missingForChannel(have)
		if missing == 0 {
			continue
		}
		if forumIsOutOfReach(ch.Type(), j.TargetType, missing) {
			evaluated--
			continue
		}
		deficits = append(deficits, channelDeficit{ChannelID: ch.ID(), Missing: missing})
	}
	if len(deficits) == 0 {
		return "", false
	}

	return renderMissingPerms(lang, deficits, evaluated), true
}

// Mirrors the purger's resolveChannels. Threads are omitted: they inherit the parent's overwrites.
func purgeTargetChannels(channels []discord.GuildChannel, j *job.PurgeJob) []discord.GuildChannel {
	skip := make(map[uint64]bool, len(j.SkipChannelIDs))
	for _, id := range j.SkipChannelIDs {
		skip[id] = true
	}

	var out []discord.GuildChannel
	for _, ch := range channels {
		switch j.TargetType {
		case job.TargetTypeChannel:
			// The engine does not apply the skip set to a single-channel target either.
			if uint64(ch.ID()) != j.TargetID {
				continue
			}
		case job.TargetTypeCategory:
			if ch.ParentID() == nil || uint64(*ch.ParentID()) != j.TargetID {
				continue
			}
			if skip[uint64(ch.ID())] {
				continue
			}
		case job.TargetTypeServer:
			if skip[uint64(ch.ID())] {
				continue
			}
		default:
			return nil
		}

		switch ch.Type() {
		case discord.ChannelTypeGuildText, discord.ChannelTypeGuildNews,
			discord.ChannelTypeGuildVoice, discord.ChannelTypeGuildForum:
			out = append(out, ch)
		}
	}
	return out
}

// Only a forum's threads are purged, and they cannot be listed without View Channel, so an
// unviewable forum contributes no work to refuse over.
func forumIsOutOfReach(kind discord.ChannelType, target job.TargetType, missing discord.Permissions) bool {
	return kind == discord.ChannelTypeGuildForum &&
		missing.Has(discord.PermissionViewChannel) &&
		target != job.TargetTypeChannel
}

func botGuildPermissions(guildID snowflake.ID, botRoleIDs []snowflake.ID, rolePerms map[snowflake.ID]discord.Permissions) discord.Permissions {
	perms := rolePerms[guildID]
	for _, id := range botRoleIDs {
		perms |= rolePerms[id]
	}
	if perms.Has(discord.PermissionAdministrator) {
		return discord.PermissionsAll
	}
	return perms
}

func botChannelPermissions(base discord.Permissions, guildID, botID snowflake.ID, botRoleIDs []snowflake.ID, overwrites discord.PermissionOverwrites) discord.Permissions {
	if base.Has(discord.PermissionAdministrator) {
		return discord.PermissionsAll
	}

	perms := base
	if ow, ok := overwrites.Role(guildID); ok {
		perms &^= ow.Deny
		perms |= ow.Allow
	}

	var allow, deny discord.Permissions
	for _, id := range botRoleIDs {
		if id == guildID {
			continue
		}
		if ow, ok := overwrites.Role(id); ok {
			allow |= ow.Allow
			deny |= ow.Deny
		}
	}
	perms &^= deny
	perms |= allow

	// Its own final layer, not folded in with the role ones: a deny aimed at the bot must beat an
	// allow it inherits from a role.
	if ow, ok := overwrites.Member(botID); ok {
		perms &^= ow.Deny
		perms |= ow.Allow
	}
	return perms
}

// Every absent bit, so a bot with nothing is told all three at once rather than one per retry.
func missingForChannel(have discord.Permissions) discord.Permissions {
	return requiredPurgePerms &^ have
}

var permissionNameTable = []struct {
	bit  discord.Permissions
	name string
}{
	{discord.PermissionViewChannel, "View Channel"},
	{discord.PermissionReadMessageHistory, "Read Message History"},
	{discord.PermissionManageMessages, "Manage Messages"},
}

// discord.Permissions.String ranges a map, so its order varies between calls.
func permissionNameList(p discord.Permissions) []string {
	names := make([]string, 0, len(permissionNameTable))
	for _, entry := range permissionNameTable {
		if p.Has(entry.bit) {
			names = append(names, entry.name)
		}
	}
	return names
}

func permissionNames(p discord.Permissions) string {
	return strings.Join(permissionNameList(p), ", ")
}

func permissionBullets(p discord.Permissions) string {
	return "\n• " + strings.Join(permissionNameList(p), "\n• ")
}

func permissionLines(p discord.Permissions) string {
	names := permissionNameList(p)
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return permissionBullets(p)
}

func renderMissingPerms(lang string, deficits []channelDeficit, evaluated int) string {
	uniform := true
	for _, d := range deficits {
		if d.Missing != deficits[0].Missing {
			uniform = false
			break
		}
	}

	// Names add nothing when none is purgeable; ahead of the cap so a large guild still collapses.
	if len(deficits) > 1 && len(deficits) == evaluated && uniform {
		return locale.MsgPurgeMissingPerms.In(lang, permissionLines(deficits[0].Missing))
	}

	if len(deficits) == 1 {
		d := deficits[0]
		return locale.MsgPurgeMissingPermsChannel.In(lang,
			discord.ChannelMention(d.ChannelID)+permissionBullets(d.Missing))
	}

	shown := deficits
	if len(shown) > maxDeficitLines {
		shown = shown[:maxDeficitLines]
	}
	lines := make([]string, 0, len(shown)+1)
	for _, d := range shown {
		lines = append(lines, locale.MsgPurgeCompleteSkippedLine.In(lang,
			discord.ChannelMention(d.ChannelID), permissionNames(d.Missing)))
	}
	if len(deficits) > len(shown) {
		lines = append(lines, "• ...")
	}
	return locale.MsgPurgeMissingPermsChannel.In(lang, "\n"+strings.Join(lines, "\n"))
}

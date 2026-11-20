package commands

import (
	"context"
	"errors"
	"slices"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"go.uber.org/zap"

	"github.com/PurgeBot-net/locale"
)

type gate int

const (
	gateOpen    gate = iota
	gateGranted      // Owner or a granted role
	gateOwner
)

var errOwnerUnresolved = errors.New("guild owner unresolved")

// Closed by default: a command added later is gated until it is listed here.
func commandGate(path string) gate {
	switch path {
	case "/help", "/cancel":
		return gateOpen
	case "/settings/access/add", "/settings/access/remove":
		return gateOwner
	}
	return gateGranted
}

// ownerID 0 means unresolved and never matches a real snowflake.
func isOwner(actorID, ownerID uint64) bool { return ownerID != 0 && actorID == ownerID }

// A failed lookup is an error, never a pass, unless the other lookup already confirmed one.
func decide(g gate, actorID, ownerID uint64, roleIDs []snowflake.ID, granted []int64, grantsErr error) (bool, error) {
	holdsGrant := grantsErr == nil && slices.ContainsFunc(roleIDs, func(id snowflake.ID) bool {
		return slices.Contains(granted, int64(id))
	})
	switch {
	case g == gateOpen, isOwner(actorID, ownerID), g == gateGranted && holdsGrant:
		return true, nil
	case ownerID == 0:
		return false, errors.Join(errOwnerUnresolved, grantsErr)
	case g == gateGranted && grantsErr != nil:
		return false, grantsErr
	}
	return false, nil
}

// Fails closed, unlike checkPurgePermissions.
func (r *Router) checkAccess(ctx context.Context, i discord.Interaction, g gate) (bool, error) {
	if g == gateOpen {
		return true, nil
	}
	if i.GuildID() == nil || i.Member() == nil {
		return false, nil
	}
	guildID := *i.GuildID()
	actorID := uint64(i.User().ID)

	if g == gateOwner {
		// A grant outlives the cached owner key, which can name a former owner after a transfer.
		return decide(g, actorID, r.fetchGuildOwnerID(ctx, guildID), nil, nil, nil)
	}

	type grants struct {
		ids []int64
		err error
	}
	lookup := make(chan grants, 1)
	go func() {
		// Runs alongside the owner lookup, so sharing its bound adds nothing to the worst case.
		dbCtx, cancel := context.WithTimeout(ctx, guildOwnerLookupTimeout)
		defer cancel()
		ids, err := r.db.GetAccessRoles(dbCtx, int64(guildID))
		lookup <- grants{ids, err}
	}()

	ownerID := r.guildOwnerID(ctx, guildID)
	// The owner never waits on a stalled grant lookup.
	if isOwner(actorID, ownerID) {
		return true, nil
	}
	res := <-lookup
	return decide(g, actorID, ownerID, i.Member().RoleIDs, res.ids, res.err)
}

// Answers on the handler's behalf when it refuses.
func (r *Router) passGate(ctx context.Context, i discord.Interaction, g gate, respond RespondFunc) bool {
	if g == gateOpen {
		return true
	}
	if i.GuildID() == nil {
		respond(ephemeralLocale(i, locale.MsgErrorGuildOnly))
		return false
	}

	ok, err := r.checkAccess(ctx, i, g)
	switch {
	case ok:
	case err != nil:
		r.logger.Error("check access", zap.Error(err), zap.String("guild_id", i.GuildID().String()))
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
	case g == gateOwner:
		respond(ephemeralContainer(locale.MsgSettingsAccessOwnerOnly.In(interactionLocale(i))))
	default:
		respond(ephemeralContainer(locale.MsgErrorNoAccess.In(interactionLocale(i))))
	}
	return ok
}

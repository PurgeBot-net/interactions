package commands

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"go.uber.org/zap"

	"github.com/PurgeBot-net/common/job"
	"github.com/PurgeBot-net/locale"
)

type cancelHandler struct{ r *Router }

func newCancelHandler(r *Router) *cancelHandler { return &cancelHandler{r} }

// Gated on job ownership and nothing else — no permission check, like the cancel button.
func (h *cancelHandler) Handle(ctx context.Context, i discord.ApplicationCommandInteraction, respond RespondFunc) {
	if i.GuildID() == nil {
		respond(ephemeralLocale(i, locale.MsgErrorGuildOnly))
		return
	}

	guildID := uint64(*i.GuildID())
	userID := uint64(i.User().ID)
	lang := interactionLocale(i)

	active, err := job.GetActiveJob(ctx, h.r.redis, guildID)
	if err != nil {
		h.r.logger.Error("get active job for cancel command", zap.Error(err))
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}
	if active != nil {
		if active.RequestedByID != userID {
			respond(ephemeralLocale(i, locale.MsgCancelNotAllowed))
			return
		}
		if err := job.Cancel(ctx, h.r.redis, active.ID); err != nil {
			h.r.logger.Error("cancel job", zap.Error(err))
			respond(ephemeralLocale(i, locale.MsgErrorInternal))
			return
		}
		respond(containerMessage(locale.MsgCancelRequested.In(lang)))
		return
	}

	pending, err := job.GetPendingJob(ctx, h.r.redis, guildID)
	if err != nil {
		h.r.logger.Error("get pending job for cancel command", zap.Error(err))
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}
	if pending == nil {
		respond(ephemeralLocale(i, locale.MsgCancelNothingRunning))
		return
	}
	if pending.RequestedByID != userID {
		respond(ephemeralLocale(i, locale.MsgCancelNotAllowed))
		return
	}

	job.DeletePendingJob(ctx, h.r.redis, guildID)
	respond(containerMessage(locale.MsgPurgeCancelledHeader.In(lang)))
}

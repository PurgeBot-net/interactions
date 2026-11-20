package commands

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"go.uber.org/zap"

	"github.com/PurgeBot-net/common/job"
	"github.com/PurgeBot-net/locale"
)

const (
	ackRetryAttempts = 5
	ackRetryDelay    = 500 * time.Millisecond
	handlerTimeout   = time.Minute
)

func commandPath(i discord.ApplicationCommandInteraction) string {
	if d, ok := i.Data.(discord.SlashCommandInteractionData); ok {
		return d.CommandPath()
	}
	return "/" + i.Data.CommandName()
}

func opensSkipUI(d discord.SlashCommandInteractionData) bool {
	return d.Bool("skip_channels") && strings.HasPrefix(d.String("target"), string(job.TargetTypeCategory)+":")
}

// The ack fixes the reply's visibility, so it follows the reply the path normally ends in.
func deferEphemeral(i discord.ApplicationCommandInteraction) bool {
	d, ok := i.Data.(discord.SlashCommandInteractionData)
	return ok && (d.CommandPath() == "/settings/access/list" || opensSkipUI(d))
}

// 10015 right after the ack means Discord has not registered it yet, not a dead token.
func retryUnregistered(call func() error) error {
	var err error
	for attempt := range ackRetryAttempts {
		if attempt > 0 {
			time.Sleep(ackRetryDelay)
		}
		if err = call(); !rest.IsJSONErrorCode(err, rest.JSONErrorCodeUnknownWebhook) {
			return err
		}
	}
	return err
}

func (r *Router) editOriginal(ctx context.Context, i discord.Interaction, u discord.MessageUpdate) error {
	return retryUnregistered(func() error {
		// disgo drains file readers into each request body, so a retry would upload empty files.
		for _, file := range u.Files {
			if s, ok := file.Reader.(io.Seeker); ok {
				s.Seek(0, io.SeekStart) //nolint:errcheck
			}
		}
		_, err := r.client.Rest.UpdateInteractionResponse(i.ApplicationID(), i.Token(), u, rest.WithCtx(ctx))
		return err
	})
}

// Acks before any handler I/O, so a slow handler can't miss Discord's 3s window.
func (r *Router) deferred(ctx context.Context, i discord.Interaction, respond RespondFunc, ephemeral bool, handle func(context.Context, RespondFunc)) {
	ack := discord.InteractionResponse{Type: discord.InteractionResponseTypeDeferredCreateMessage}
	if ephemeral {
		ack.Data = discord.MessageCreate{Flags: discord.MessageFlagEphemeral}
	}
	respond(ack)

	f := &followup{r: r, i: i, ephemeral: ephemeral, ctx: context.WithoutCancel(ctx)}
	r.inflight.Go(func() {
		defer f.finish()
		// Bounds a stalled query or download; f.ctx keeps no deadline so the reply still goes out.
		handlerCtx, cancel := context.WithTimeout(f.ctx, handlerTimeout)
		defer cancel()
		handle(handlerCtx, f.respond)
	})
}

type followup struct {
	r         *Router
	i         discord.Interaction
	ephemeral bool
	// No deadline: disgo fails a request whose deadline falls before a bucket reset.
	ctx  context.Context
	sent bool
}

func (f *followup) respond(resp discord.InteractionResponse) {
	f.sent = true
	switch resp.Type {
	case discord.InteractionResponseTypeDeferredCreateMessage:
		// The handler edits @original itself.
	case discord.InteractionResponseTypeCreateMessage:
		m, _ := resp.Data.(discord.MessageCreate)
		f.deliver(m)
	default:
		f.r.logger.Error("response type cannot follow a defer", zap.Int("type", int(resp.Type)))
	}
}

func (f *followup) deliver(m discord.MessageCreate) {
	var err error
	if m.Flags.Has(discord.MessageFlagEphemeral) == f.ephemeral {
		err = f.r.editOriginal(f.ctx, f.i, toUpdate(m))
	} else if err = retryUnregistered(func() error {
		return f.r.client.Rest.DeleteInteractionResponse(f.i.ApplicationID(), f.i.Token(), rest.WithCtx(f.ctx))
	}); err == nil {
		err = f.createFollowup(m)
	} else {
		f.r.logger.Warn("delete loading reply", zap.Error(err))
		// A followup sent while the reply still loads becomes an edit of it, which would show m
		// with the ack's visibility. A neutral text clears the loading state first.
		msg := locale.MsgErrorInternal.In(interactionLocale(f.i))
		if err = f.r.editOriginal(f.ctx, f.i, discord.MessageUpdate{Content: &msg, AllowedMentions: noPings}); err == nil {
			err = f.createFollowup(m)
		}
	}
	if err != nil {
		f.r.logger.Error("answer deferred interaction", zap.Error(err))
	}
}

func (f *followup) createFollowup(m discord.MessageCreate) error {
	return retryUnregistered(func() error {
		_, err := f.r.client.Rest.CreateFollowupMessage(f.i.ApplicationID(), f.i.Token(), m, rest.WithCtx(f.ctx))
		return err
	})
}

func (f *followup) finish() {
	if p := recover(); p != nil {
		f.r.logger.Error("interaction handler panicked", zap.Any("panic", p), zap.Stack("stack"))
	}
	// Otherwise the loading reply spins until the token expires.
	if !f.sent {
		f.respond(ephemeralLocale(f.i, locale.MsgErrorInternal))
	}
}

// Content must stay nil on a Components V2 message.
func toUpdate(m discord.MessageCreate) discord.MessageUpdate {
	u := discord.MessageUpdate{AllowedMentions: m.AllowedMentions}
	if m.Content != "" {
		u.Content = &m.Content
	}
	if len(m.Components) > 0 {
		u.Components = &m.Components
	}
	if flags := m.Flags.Remove(discord.MessageFlagEphemeral); flags != 0 {
		u.Flags = &flags
	}
	return u
}

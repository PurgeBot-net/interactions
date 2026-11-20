package commands

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	chart "github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
	"go.uber.org/zap"

	"github.com/PurgeBot-net/database"
	"github.com/PurgeBot-net/locale"
)

type statsHandler struct{ r *Router }

func newStatsHandler(r *Router) *statsHandler { return &statsHandler{r} }

func (h *statsHandler) Handle(ctx context.Context, i discord.ApplicationCommandInteraction, respond RespondFunc) {
	if i.GuildID() == nil {
		respond(ephemeralLocale(i, locale.MsgErrorGuildOnly))
		return
	}

	// Before the Premium gate, so an opted-out server is not shown an upsell for stats it turned off.
	settings, err := h.r.db.GetGuildSettings(ctx, int64(*i.GuildID()))
	if err != nil {
		h.r.logger.Error("get guild settings", zap.Error(err))
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}
	if settings != nil && !settings.StatsEnabled {
		respond(ephemeral(locale.MsgStatsOptedOut.In(interactionLocale(i))))
		return
	}

	premium, err := h.r.hasPremium(ctx, *i.GuildID())
	if err != nil {
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}
	if !premium {
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

	data := i.SlashCommandInteractionData()
	days := 0
	if d, ok := data.OptInt("days"); ok {
		days = d
	}

	respond(discord.InteractionResponse{
		Type: discord.InteractionResponseTypeDeferredCreateMessage,
	})

	lang := interactionLocale(i)
	guildID := *i.GuildID()

	updateErr := func(msg locale.Message) {
		flags := discord.MessageFlagIsComponentsV2
		_ = h.r.editOriginal(ctx, i, discord.MessageUpdate{
			Components: &[]discord.LayoutComponent{
				discord.NewContainer(discord.NewTextDisplay(msg.In(lang))),
			},
			Flags: &flags,
		})
	}

	stats, err := h.r.db.GetGuildStats(ctx, int64(guildID), days)
	if err != nil {
		h.r.logger.Error("get guild stats", zap.Error(err))
		updateErr(locale.MsgErrorInternal)
		return
	}

	lines := []string{locale.MsgStatsHeader.In(lang)}

	if stats.TotalPurges == 0 {
		lines = append(lines, locale.MsgStatsNoPurges.In(lang))
		flags := discord.MessageFlagIsComponentsV2
		_ = h.r.editOriginal(ctx, i, discord.MessageUpdate{
			Components: &[]discord.LayoutComponent{discord.NewContainer(discord.NewTextDisplay(strings.Join(lines, "\n")))},
			Flags:      &flags,
		})
		return
	}

	lines = append(lines, locale.MsgStatsTotals.In(lang, stats.TotalPurges, stats.TotalDeleted))
	if stats.LastPurgeAt != nil {
		lines = append(lines, locale.MsgStatsLastPurge.In(lang, fmt.Sprintf("<t:%d:R>", stats.LastPurgeAt.Unix())))
	}

	card := []discord.ContainerSubComponent{discord.NewTextDisplay(strings.Join(lines, "\n"))}
	var files []*discord.File

	if stats.TotalDeleted > 0 {
		png, chartErr := generateStatsChart(stats.ChartBars, days, stats.Monthly)
		if chartErr != nil {
			h.r.logger.Warn("generate stats chart", zap.Error(chartErr))
		} else {
			card = append(card, discord.NewMediaGallery(
				discord.MediaGalleryItem{
					Media: discord.UnfurledMediaItem{URL: "attachment://stats.png"},
				},
			))
			files = []*discord.File{discord.NewFile("stats.png", "Purge activity chart", bytes.NewReader(png))}
		}
	}

	components := []discord.LayoutComponent{discord.NewContainer(card...)}
	flags := discord.MessageFlagIsComponentsV2
	if err := h.r.editOriginal(ctx, i, discord.MessageUpdate{
		Components: &components,
		Files:      files,
		Flags:      &flags,
	}); err != nil {
		h.r.logger.Warn("update stats response", zap.Error(err))
	}
}

func generateStatsChart(data []database.DailyPurgeStat, days int, monthly bool) ([]byte, error) {
	var (
		bgColor   = drawing.ColorFromHex("313338")
		barColor  = drawing.ColorFromHex("5865F2")
		textColor = drawing.ColorFromHex("B5BAC1")
		gridColor = drawing.ColorFromHex("404249")
	)

	var title, labelFmt string
	var labelInterval int

	if monthly {
		title = "Messages Deleted — Last 12 Months"
		labelFmt = "Jan '06"
		labelInterval = 1
	} else {
		title = fmt.Sprintf("Messages Deleted — Last %d Days", days)
		labelFmt = "Jan 2"
		labelInterval = days / 7
		if labelInterval < 1 {
			labelInterval = 1
		}
	}

	bars := make([]chart.Value, len(data))
	for i, d := range data {
		label := ""
		if i%labelInterval == 0 || i == len(data)-1 {
			label = d.Date.Format(labelFmt)
		}
		bars[i] = chart.Value{
			Value: float64(d.Deleted),
			Label: label,
			Style: chart.Style{
				FillColor:   barColor,
				StrokeColor: barColor,
			},
		}
	}

	width := 800
	if !monthly && days > 30 {
		width = 1000
	}
	barWidth := width/len(bars) - 4
	if barWidth < 4 {
		barWidth = 4
	}
	if barWidth > 40 {
		barWidth = 40
	}

	bc := chart.BarChart{
		Title:      title,
		TitleStyle: chart.Style{FontColor: textColor, FontSize: 14},
		Background: chart.Style{FillColor: bgColor, StrokeColor: bgColor},
		Canvas:     chart.Style{FillColor: bgColor},
		Width:      width,
		Height:     400,
		BarWidth:   barWidth,
		XAxis:      chart.Style{FontColor: textColor, StrokeColor: gridColor},
		YAxis: chart.YAxis{
			Style: chart.Style{FontColor: textColor, StrokeColor: gridColor},
		},
		Bars: bars,
	}

	buf := &bytes.Buffer{}
	if err := bc.Render(chart.PNG, buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

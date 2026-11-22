package commands

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
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
		respond(h.r.premiumUpsell(i, locale.MsgErrorNoPremium))
		return
	}

	days := 0
	if d, ok := i.SlashCommandInteractionData().OptInt("days"); ok {
		days = d
	}
	lang := interactionLocale(i)

	stats, err := h.r.db.GetGuildStats(ctx, int64(*i.GuildID()), days)
	if err != nil {
		h.r.logger.Error("get guild stats", zap.Error(err))
		respond(ephemeralLocale(i, locale.MsgErrorInternal))
		return
	}

	lines := []string{locale.MsgStatsHeader.In(lang)}

	if stats.TotalPurges == 0 {
		lines = append(lines, locale.MsgStatsNoPurges.In(lang))
		respond(containerMessage(strings.Join(lines, "\n")))
		return
	}

	lines = append(lines, locale.MsgStatsTotals.In(lang, stats.TotalPurges, stats.TotalDeleted))
	if stats.LastPurgeAt != nil {
		lines = append(lines, locale.MsgStatsLastPurge.In(lang, fmt.Sprintf("<t:%d:R>", stats.LastPurgeAt.Unix())))
	}

	card := []discord.ContainerSubComponent{discord.NewTextDisplay(strings.Join(lines, "\n"))}
	var files []*discord.File

	png, chartErr := generateStatsChart(stats.ChartBars, days, stats.Monthly)
	if chartErr != nil {
		h.r.logger.Warn("generate stats chart", zap.Error(chartErr))
	} else if png != nil {
		card = append(card, discord.NewMediaGallery(
			discord.MediaGalleryItem{
				Media: discord.UnfurledMediaItem{URL: "attachment://stats.png"},
			},
		))
		files = []*discord.File{discord.NewFile("stats.png", "Purge activity chart", bytes.NewReader(png))}
	}

	respond(discord.InteractionResponse{
		Type: discord.InteractionResponseTypeCreateMessage,
		Data: discord.NewMessageCreateV2(discord.NewContainer(card...)).WithFiles(files...),
	})
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
		labelInterval = max(days/7, 1)
	}

	var peak int64
	for _, d := range data {
		peak = max(peak, d.Deleted)
	}
	if peak == 0 {
		return nil, nil
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
	barWidth := min(max(width/len(bars)-4, 4), 40)

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
			// Without a range go-chart spans min..max, drawing the smallest bar at zero height.
			Range: &chart.ContinuousRange{Min: 0, Max: float64(peak)},
		},
		Bars: bars,
	}

	buf := &bytes.Buffer{}
	if err := bc.Render(chart.PNG, buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

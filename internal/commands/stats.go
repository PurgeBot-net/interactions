package commands

import (
	"bytes"
	"context"
	"fmt"

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

	if !h.r.hasPremium(ctx, *i.GuildID()) {
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
	appID := i.ApplicationID()
	token := i.Token()
	guildID := *i.GuildID()

	// must outlive the HTTP request
	workCtx := context.WithoutCancel(ctx)

	go func() {
		updateErr := func(msg locale.Message) {
			flags := discord.MessageFlagIsComponentsV2
			_, _ = h.r.client.Rest.UpdateInteractionResponse(appID, token, discord.MessageUpdate{
				Components: &[]discord.LayoutComponent{
					discord.NewContainer(discord.NewTextDisplay(msg.In(lang))),
				},
				Flags: &flags,
			})
		}

		stats, err := h.r.db.GetGuildStats(workCtx, int64(guildID), days)
		if err != nil {
			h.r.logger.Error("get guild stats", zap.Error(err))
			updateErr(locale.MsgErrorInternal)
			return
		}

		texts := []discord.ContainerSubComponent{
			discord.NewTextDisplay(locale.MsgStatsHeader.In(lang)),
		}

		if stats.TotalPurges == 0 {
			texts = append(texts, discord.NewTextDisplay(locale.MsgStatsNoPurges.In(lang)))
			flags := discord.MessageFlagIsComponentsV2
			_, _ = h.r.client.Rest.UpdateInteractionResponse(appID, token, discord.MessageUpdate{
				Components: &[]discord.LayoutComponent{discord.NewContainer(texts...)},
				Flags:      &flags,
			})
			return
		}

		texts = append(texts,
			discord.NewTextDisplay(locale.MsgStatsTotals.In(lang, stats.TotalPurges, stats.TotalDeleted)),
		)
		if stats.LastPurgeAt != nil {
			texts = append(texts, discord.NewTextDisplay(locale.MsgStatsLastPurge.In(lang, fmt.Sprintf("<t:%d:R>", stats.LastPurgeAt.Unix()))))
		}

		components := []discord.LayoutComponent{discord.NewContainer(texts...)}
		var files []*discord.File

		if stats.TotalDeleted > 0 {
			png, chartErr := generateStatsChart(stats.ChartBars, days, stats.Monthly)
			if chartErr != nil {
				h.r.logger.Warn("generate stats chart", zap.Error(chartErr))
			} else {
				components = append(components, discord.NewMediaGallery(
					discord.MediaGalleryItem{
						Media: discord.UnfurledMediaItem{URL: "attachment://stats.png"},
					},
				))
				files = []*discord.File{discord.NewFile("stats.png", "Purge activity chart", bytes.NewReader(png))}
			}
		}

		flags := discord.MessageFlagIsComponentsV2
		if _, err := h.r.client.Rest.UpdateInteractionResponse(appID, token, discord.MessageUpdate{
			Components: &components,
			Files:      files,
			Flags:      &flags,
		}); err != nil {
			h.r.logger.Warn("update stats response", zap.Error(err))
		}
	}()
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

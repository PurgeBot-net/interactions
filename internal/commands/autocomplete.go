package commands

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

const (
	maxChoices    = 25
	maxChoiceName = 100
)

type targetChannel struct {
	ID       snowflake.ID
	Name     string
	Kind     discord.ChannelType
	ParentID snowflake.ID
	Position int
}

// category is nil for the uncategorized run.
type choiceGroup struct {
	category *targetChannel
	children []targetChannel
}

func channelRows(channels []discord.GuildChannel) []targetChannel {
	rows := make([]targetChannel, len(channels))
	for i, ch := range channels {
		rows[i] = targetChannel{
			ID:       ch.ID(),
			Name:     ch.Name(),
			Kind:     ch.Type(),
			Position: ch.Position(),
		}
		if parent := ch.ParentID(); parent != nil {
			rows[i].ParentID = *parent
		}
	}
	return rows
}

func targetChoices(rows []targetChannel, query string, guildID snowflake.ID, excludeServer bool) []discord.AutocompleteChoice {
	query = strings.ToLower(query)

	var categories []targetChannel
	known := map[snowflake.ID]bool{}
	for _, ch := range rows {
		if ch.Kind == discord.ChannelTypeGuildCategory {
			categories = append(categories, ch)
			known[ch.ID] = true
		}
	}

	children := map[snowflake.ID][]targetChannel{}
	for _, ch := range rows {
		if !isPurgeableChannel(ch.Kind) {
			continue
		}
		parent := ch.ParentID
		// Discord omits categories the bot cannot view, so a child can outlive its parent.
		if !known[parent] {
			parent = 0
		}
		children[parent] = append(children[parent], ch)
	}

	slices.SortStableFunc(categories, bySidebarOrder)
	for _, group := range children {
		slices.SortStableFunc(group, bySidebarOrder)
	}

	groups := make([]choiceGroup, 0, len(categories)+1)
	if loose := matchingChannels(children[0], query); len(loose) > 0 {
		groups = append(groups, choiceGroup{children: loose})
	}
	for _, cat := range categories {
		kids := children[cat.ID]
		if len(kids) == 0 {
			continue
		}
		matched := matchingChannels(kids, query)
		if len(matched) == 0 && !matches(cat.Name, query) {
			continue
		}
		groups = append(groups, choiceGroup{category: &cat, children: matched})
	}

	includeServer := !excludeServer && matches("server", query)
	take := groupBudget(groups, includeServer)

	choices := make([]discord.AutocompleteChoice, 0, maxChoices)
	if includeServer {
		choices = append(choices, discord.AutocompleteChoiceString{
			Name:  choiceName("📍 ", "Server"),
			Value: "server:" + guildID.String(),
		})
	}
	for i, g := range groups {
		prefix := ""
		if g.category != nil {
			choices = append(choices, discord.AutocompleteChoiceString{
				Name:  choiceName("📁 ", g.category.Name),
				Value: "category:" + g.category.ID.String(),
			})
			prefix = "↳ "
		}
		for _, ch := range g.children[:take[i]] {
			choices = append(choices, discord.AutocompleteChoiceString{
				Name:  choiceName(prefix+channelPrefix(ch.Kind), ch.Name),
				Value: "channel:" + ch.ID.String(),
			})
		}
	}

	if len(choices) > maxChoices {
		choices = choices[:maxChoices]
	}
	return choices
}

// Headers are reserved a slot before children get one, or a single large category buries every
// category below it in the 25 Discord allows.
func groupBudget(groups []choiceGroup, includeServer bool) []int {
	take := make([]int, len(groups))

	total, headers := 0, 0
	for _, g := range groups {
		total += len(g.children)
		if g.category != nil {
			headers++
		}
	}
	total += headers
	if includeServer {
		total++
	}

	if total <= maxChoices {
		for i, g := range groups {
			take[i] = len(g.children)
		}
		return take
	}

	budget := maxChoices - headers
	if includeServer {
		budget--
	}
	for budget > 0 {
		progressed := false
		for i, g := range groups {
			if budget == 0 {
				break
			}
			if take[i] < len(g.children) {
				take[i]++
				budget--
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}
	return take
}

func matchingChannels(channels []targetChannel, query string) []targetChannel {
	out := make([]targetChannel, 0, len(channels))
	for _, ch := range channels {
		if matches(ch.Name, query) {
			out = append(out, ch)
		}
	}
	return out
}

// Voice channels are numbered in a sequence of their own, so Position alone interleaves them into
// the text run in an order the client never shows. Ties go to the lower ID, as they do in Discord.
func bySidebarOrder(a, b targetChannel) int {
	if r := cmp.Compare(sidebarRank(a.Kind), sidebarRank(b.Kind)); r != 0 {
		return r
	}
	if r := cmp.Compare(a.Position, b.Position); r != 0 {
		return r
	}
	return cmp.Compare(a.ID, b.ID)
}

func sidebarRank(t discord.ChannelType) int {
	if t == discord.ChannelTypeGuildVoice {
		return 1
	}
	return 0
}

// One name over 100 characters makes Discord reject the whole response, and the bot never learns
// of it: the choices are the body of its own reply, so the 400 reaches the user as an empty list.
func choiceName(prefix, name string) string {
	full := prefix + name
	if utf8.RuneCountInString(full) <= maxChoiceName {
		return full
	}
	return string([]rune(full)[:maxChoiceName-1]) + "…"
}

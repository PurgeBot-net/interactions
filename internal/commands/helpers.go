package commands

import (
	"strings"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"

	"github.com/PurgeBot-net/locale"
)

func interactionLocale(i discord.Interaction) string {
	if gl := i.GuildLocale(); gl != nil {
		return string(*gl)
	}
	return string(i.Locale())
}

func ephemeralLocale(i discord.Interaction, key locale.Message) discord.InteractionResponse {
	return ephemeral(key.In(interactionLocale(i)))
}

func publicLocale(i discord.Interaction, key locale.Message) discord.InteractionResponse {
	return containerMessage(key.In(interactionLocale(i)))
}

func ephemeral(text string) discord.InteractionResponse {
	return discord.InteractionResponse{
		Type: discord.InteractionResponseTypeCreateMessage,
		Data: ephemeralContainerCreate(text),
	}
}

func containerMessage(text string) discord.InteractionResponse {
	return discord.InteractionResponse{
		Type: discord.InteractionResponseTypeCreateMessage,
		Data: discord.MessageCreate{
			Flags:      discord.MessageFlagIsComponentsV2,
			Components: []discord.LayoutComponent{discord.NewContainer(discord.NewTextDisplay(text))},
		},
	}
}

func ephemeralContainerCreate(text string) discord.MessageCreate {
	return discord.MessageCreate{
		Flags:      discord.MessageFlagIsComponentsV2 | discord.MessageFlagEphemeral,
		Components: []discord.LayoutComponent{discord.NewContainer(discord.NewTextDisplay(text))},
	}
}

// Echoed input and channel names have no length cap of their own, and Discord refuses the whole message.
func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max-1]) + "…"
}

func matches(name, query string) bool {
	return query == "" || strings.Contains(strings.ToLower(name), query)
}

func isPurgeableChannel(t discord.ChannelType) bool {
	switch t {
	case discord.ChannelTypeGuildText, discord.ChannelTypeGuildNews,
		discord.ChannelTypeGuildVoice, discord.ChannelTypeGuildForum:
		return true
	}
	return false
}

// Only the kinds a purge treats differently earn a mark; text reads as the client writes it.
func channelPrefix(t discord.ChannelType) string {
	switch t {
	case discord.ChannelTypeGuildNews:
		return "📢 "
	case discord.ChannelTypeGuildVoice:
		return "🔊 "
	case discord.ChannelTypeGuildForum:
		return "💭 "
	default:
		return "#"
	}
}

func autocompleteResult(choices []discord.AutocompleteChoice) discord.InteractionResponse {
	return discord.InteractionResponse{
		Type: discord.InteractionResponseTypeAutocompleteResult,
		Data: discord.AutocompleteResult{Choices: choices},
	}
}

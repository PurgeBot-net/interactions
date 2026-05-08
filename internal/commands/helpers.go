package commands

import (
	"strings"

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
	return discord.InteractionResponse{
		Type: discord.InteractionResponseTypeCreateMessage,
		Data: discord.NewMessageCreate().WithContent(key.In(interactionLocale(i))),
	}
}

func ephemeral(msg string) discord.InteractionResponse {
	return discord.InteractionResponse{
		Type: discord.InteractionResponseTypeCreateMessage,
		Data: discord.NewMessageCreate().WithContent(msg).WithFlags(discord.MessageFlagEphemeral),
	}
}

func matches(name, query string) bool {
	return query == "" || strings.Contains(strings.ToLower(name), query)
}

func channelEmoji(t discord.ChannelType) string {
	switch t {
	case discord.ChannelTypeGuildNews:
		return "📢 "
	case discord.ChannelTypeGuildVoice:
		return "🔊 "
	case discord.ChannelTypeGuildForum:
		return "💭 "
	default:
		return "💬 "
	}
}

func autocompleteResult(choices []discord.AutocompleteChoice) discord.InteractionResponse {
	return discord.InteractionResponse{
		Type: discord.InteractionResponseTypeAutocompleteResult,
		Data: discord.AutocompleteResult{Choices: choices},
	}
}

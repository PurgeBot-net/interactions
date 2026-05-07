package commands

import "github.com/disgoorg/disgo/discord"

// GlobalCommands returns all commands registered globally.
func GlobalCommands() []discord.ApplicationCommandCreate {
	return []discord.ApplicationCommandCreate{
		purgeCommand(),
		helpCommand(),
		customizeCommand(),
		statsCommand(),
	}
}

func purgeCommand() discord.ApplicationCommandCreate {
	return discord.SlashCommandCreate{
		Name:        "purge",
		Description: "Delete messages in bulk",
		Options: []discord.ApplicationCommandOption{
			purgeSubcommand("user", "Delete messages from a specific user",
				discord.ApplicationCommandOptionUser{Name: "user", Description: "Target user", Required: true},
			),
			purgeSubcommand("role", "Delete messages from members with a role",
				discord.ApplicationCommandOptionRole{Name: "role", Description: "Target role", Required: true},
				discord.ApplicationCommandOptionBool{Name: "include_bots", Description: "Include bot messages"},
			),
			purgeSubcommand("everyone", "Delete all messages in the target",
				discord.ApplicationCommandOptionBool{Name: "include_bots", Description: "Include bot messages"},
			),
			purgeSubcommand("inactive", "Delete messages from users who left",
				discord.ApplicationCommandOptionBool{Name: "include_bots", Description: "Include bot messages"},
			),
			purgeSubcommand("webhook", "Delete all webhook messages"),
			purgeSubcommand("deleted", "Delete messages from deleted accounts"),
		},
	}
}

// purgeSubcommand builds a /purge subcommand with the shared options appended.
// Required options are always placed before optional ones to satisfy Discord's validation.
func purgeSubcommand(name, description string, extra ...discord.ApplicationCommandOption) discord.ApplicationCommandOptionSubCommand {
	required := []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionString{
			Name:         "target_id",
			Description:  "Server, category, or channel to purge",
			Required:     true,
			Autocomplete: true,
		},
	}
	optional := []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionInt{
			Name:        "days",
			Description: "Only delete messages from the last X days (1–30)",
			MinValue:    ptr(1),
			MaxValue:    ptr(30),
		},
		discord.ApplicationCommandOptionString{Name: "filter", Description: "Text or regex pattern to match"},
		discord.ApplicationCommandOptionString{
			Name:        "filter_mode",
			Description: "How to apply the filter",
			Choices: []discord.ApplicationCommandOptionChoiceString{
				{Name: "contains", Value: "contains"},
				{Name: "regex", Value: "regex"},
				{Name: "exact", Value: "exact"},
				{Name: "starts_with", Value: "starts_with"},
				{Name: "ends_with", Value: "ends_with"},
			},
		},
		discord.ApplicationCommandOptionBool{Name: "case_sensitive", Description: "Case-sensitive filter"},
		discord.ApplicationCommandOptionBool{Name: "include_threads", Description: "Include thread messages"},
		discord.ApplicationCommandOptionBool{Name: "skip_channels", Description: "Interactively choose channels to skip (category only)"},
	}

	for _, opt := range extra {
		if optionIsRequired(opt) {
			required = append(required, opt)
		} else {
			optional = append(optional, opt)
		}
	}

	return discord.ApplicationCommandOptionSubCommand{
		Name:        name,
		Description: description,
		Options:     append(required, optional...),
	}
}

func optionIsRequired(opt discord.ApplicationCommandOption) bool {
	switch o := opt.(type) {
	case discord.ApplicationCommandOptionString:
		return o.Required
	case discord.ApplicationCommandOptionInt:
		return o.Required
	case discord.ApplicationCommandOptionBool:
		return o.Required
	case discord.ApplicationCommandOptionUser:
		return o.Required
	case discord.ApplicationCommandOptionRole:
		return o.Required
	case discord.ApplicationCommandOptionChannel:
		return o.Required
	default:
		return false
	}
}

func helpCommand() discord.ApplicationCommandCreate {
	return discord.SlashCommandCreate{
		Name:        "help",
		Description: "Show bot info and command documentation",
	}
}

func customizeCommand() discord.ApplicationCommandCreate {
	return discord.SlashCommandCreate{
		Name:        "customize",
		Description: "Customize the bot appearance for this server (Premium)",
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionSubCommand{
				Name:        "edit",
				Description: "Set a custom bot name, avatar, and branding",
			},
			discord.ApplicationCommandOptionSubCommand{
				Name:        "clear",
				Description: "Reset bot name and avatar to defaults",
			},
		},
	}
}

func statsCommand() discord.ApplicationCommandCreate {
	return discord.SlashCommandCreate{
		Name:        "stats",
		Description: "View purge statistics for this server (Premium)",
	}
}

func ptr[T any](v T) *T { return &v }

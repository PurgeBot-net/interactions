package commands

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

func testCategory(id snowflake.ID, name string, position int) targetChannel {
	return targetChannel{ID: id, Name: name, Kind: discord.ChannelTypeGuildCategory, Position: position}
}

func testText(id snowflake.ID, name string, parent snowflake.ID, position int) targetChannel {
	return targetChannel{ID: id, Name: name, Kind: discord.ChannelTypeGuildText, ParentID: parent, Position: position}
}

func testVoice(id snowflake.ID, name string, parent snowflake.ID, position int) targetChannel {
	return targetChannel{ID: id, Name: name, Kind: discord.ChannelTypeGuildVoice, ParentID: parent, Position: position}
}

func choiceNames(choices []discord.AutocompleteChoice) []string {
	out := make([]string, len(choices))
	for i, c := range choices {
		out[i] = c.ChoiceName()
	}
	return out
}

func choiceValues(choices []discord.AutocompleteChoice) []string {
	out := make([]string, len(choices))
	for i, c := range choices {
		out[i] = c.(discord.AutocompleteChoiceString).Value
	}
	return out
}

func TestTargetChoicesOrdersServerThenUncategorizedThenCategories(t *testing.T) {
	channels := []targetChannel{
		testCategory(20, "Support", 1),
		testText(12, "help", 20, 0),
		testCategory(10, "General", 0),
		testText(11, "rules", 10, 0),
		testText(13, "welcome", 0, 0),
	}

	got := choiceNames(targetChoices(channels, "", testGuildID, false))
	want := []string{
		"📍 Server",
		"#welcome",
		"📁 General",
		"↳ #rules",
		"📁 Support",
		"↳ #help",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the list does not read like the Discord sidebar: %#v", got)
	}
}

// Voice at position 0 outranks text at position 1 numerically, so only the kind split saves it.
func TestTargetChoicesSortsTextBeforeVoiceRegardlessOfPosition(t *testing.T) {
	channels := []targetChannel{
		testCategory(10, "General", 0),
		testVoice(12, "Lounge", 10, 0),
		testText(11, "chat", 10, 1),
	}

	got := choiceNames(targetChoices(channels, "", testGuildID, false))
	want := []string{"📍 Server", "📁 General", "↳ #chat", "↳ 🔊 Lounge"}
	if !slices.Equal(got, want) {
		t.Fatalf("a lower voice position must not jump above the text run: %#v", got)
	}
}

func TestTargetChoicesOrdersEachGroupByPosition(t *testing.T) {
	channels := []targetChannel{
		testText(13, "zulu", 0, 2),
		testText(11, "alpha", 0, 0),
		testText(12, "mike", 0, 1),
	}

	got := choiceNames(targetChoices(channels, "", testGuildID, false))
	want := []string{"📍 Server", "#alpha", "#mike", "#zulu"}
	if !slices.Equal(got, want) {
		t.Fatalf("the uncategorized group ignored Position: %#v", got)
	}
}

func TestTargetChoicesBreaksEqualPositionsByAscendingID(t *testing.T) {
	channels := []targetChannel{
		testCategory(30, "Beta", 0),
		testText(31, "beta-chat", 30, 0),
		testCategory(20, "Alpha", 0),
		testText(21, "alpha-chat", 20, 0),
	}

	got := choiceNames(targetChoices(channels, "", testGuildID, false))
	want := []string{"📍 Server", "📁 Alpha", "↳ #alpha-chat", "📁 Beta", "↳ #beta-chat"}
	if !slices.Equal(got, want) {
		t.Fatalf("equal positions must fall back to the snowflake, not the response order: %#v", got)
	}
}

func TestTargetChoicesKeepsTheGroupedShapeWhileSearching(t *testing.T) {
	channels := []targetChannel{
		testCategory(10, "General", 0),
		testText(11, "rules", 10, 0),
		testText(12, "chat", 10, 1),
		testText(13, "welcome", 0, 0),
	}

	got := choiceNames(targetChoices(channels, "ch", testGuildID, false))
	want := []string{"📁 General", "↳ #chat"}
	if !slices.Equal(got, want) {
		t.Fatalf("a search must stay grouped rather than flatten: %#v", got)
	}
}

func TestTargetChoicesShowsACategoryAloneWhenOnlyItsNameMatches(t *testing.T) {
	channels := []targetChannel{
		testCategory(10, "General", 0),
		testText(11, "rules", 10, 0),
	}

	got := choiceNames(targetChoices(channels, "gener", testGuildID, false))
	want := []string{"📁 General"}
	if !slices.Equal(got, want) {
		t.Fatalf("a matching category is a valid target even with no matching child: %#v", got)
	}
}

func TestTargetChoicesShowsTheHeaderOfACategoryWhoseChildMatches(t *testing.T) {
	channels := []targetChannel{
		testCategory(10, "General", 0),
		testText(11, "rules", 10, 0),
	}

	got := choiceNames(targetChoices(channels, "rul", testGuildID, false))
	want := []string{"📁 General", "↳ #rules"}
	if !slices.Equal(got, want) {
		t.Fatalf("an indented row was left with no header above it: %#v", got)
	}
}

func TestTargetChoicesHidesACategoryWithNoPurgeableChildren(t *testing.T) {
	channels := []targetChannel{
		testCategory(10, "Empty", 0),
		{ID: 11, Name: "stage", Kind: discord.ChannelTypeGuildStageVoice, ParentID: 10},
	}

	got := choiceNames(targetChoices(channels, "", testGuildID, false))
	want := []string{"📍 Server"}
	if !slices.Equal(got, want) {
		t.Fatalf("a category with nothing purgeable under it is not a target: %#v", got)
	}
}

func TestTargetChoicesSkipsChannelKindsThePurgerCannotHandle(t *testing.T) {
	channels := []targetChannel{
		testText(11, "chat", 0, 0),
		{ID: 12, Name: "stage", Kind: discord.ChannelTypeGuildStageVoice, Position: 1},
		{ID: 13, Name: "gallery", Kind: discord.ChannelTypeGuildMedia, Position: 2},
		{ID: 14, Name: "thread", Kind: discord.ChannelTypeGuildPublicThread, ParentID: 11},
	}

	got := choiceNames(targetChoices(channels, "", testGuildID, false))
	want := []string{"📍 Server", "#chat"}
	if !slices.Equal(got, want) {
		t.Fatalf("an unpurgeable kind leaked into the list: %#v", got)
	}
}

func TestTargetChoicesOmitsTheServerRowWhenTheSubcommandExcludesIt(t *testing.T) {
	channels := []targetChannel{testText(11, "chat", 0, 0)}

	got := choiceNames(targetChoices(channels, "", testGuildID, true))
	want := []string{"#chat"}
	if !slices.Equal(got, want) {
		t.Fatalf("/purge everyone must not offer a server-wide target: %#v", got)
	}
}

// Discord's channel obfuscation drops categories the bot cannot view while keeping a child it can.
func TestTargetChoicesTreatsAChildOfAMissingCategoryAsUncategorized(t *testing.T) {
	channels := []targetChannel{
		testText(11, "staff-logs", 99, 0),
		testText(12, "rules", 0, 1),
	}

	got := choiceNames(targetChoices(channels, "", testGuildID, false))
	want := []string{"📍 Server", "#staff-logs", "#rules"}
	if !slices.Equal(got, want) {
		t.Fatalf("an orphaned child was dropped instead of listed: %#v", got)
	}
}

func TestTargetChoicesEncodesTheKindAndIDWithoutTheIndent(t *testing.T) {
	channels := []targetChannel{
		testCategory(10, "General", 0),
		testText(11, "chat", 10, 0),
	}

	got := choiceValues(targetChoices(channels, "", testGuildID, false))
	want := []string{"server:1", "category:10", "channel:11"}
	if !slices.Equal(got, want) {
		t.Fatalf("the label prefix leaked into the value: %#v", got)
	}
}

func TestTargetChoicesKeepsEveryCategoryReachableWhenTheListOverflows(t *testing.T) {
	channels := []targetChannel{
		testCategory(100, "Big", 0),
		testCategory(200, "Middle", 1),
		testCategory(300, "Small", 2),
	}
	for i := range 30 {
		channels = append(channels, testText(snowflake.ID(1000+i), fmt.Sprintf("big-%d", i), 100, i))
	}
	for i := range 4 {
		channels = append(channels, testText(snowflake.ID(2000+i), fmt.Sprintf("mid-%d", i), 200, i))
	}
	for i := range 5 {
		channels = append(channels, testText(snowflake.ID(3000+i), fmt.Sprintf("small-%d", i), 300, i))
	}

	got := choiceNames(targetChoices(channels, "", testGuildID, false))
	if len(got) != maxChoices {
		t.Fatalf("the budget left %d of %d slots unused: %#v", maxChoices-len(got), maxChoices, got)
	}
	for _, header := range []string{"📁 Big", "📁 Middle", "📁 Small"} {
		if !slices.Contains(got, header) {
			t.Fatalf("one large category buried %q: %#v", header, got)
		}
	}
}

func TestTargetChoicesFallsBackToHeadersWhenCategoriesAloneOverflow(t *testing.T) {
	var channels []targetChannel
	for i := range 30 {
		cat := snowflake.ID(100 + i)
		channels = append(channels, testCategory(cat, fmt.Sprintf("cat-%02d", i), i))
		channels = append(channels, testText(snowflake.ID(1000+i), fmt.Sprintf("chat-%02d", i), cat, 0))
	}

	got := choiceNames(targetChoices(channels, "", testGuildID, false))
	if len(got) != maxChoices {
		t.Fatalf("expected exactly %d choices, got %d: %#v", maxChoices, len(got), got)
	}
	for _, name := range got[1:] {
		if !strings.HasPrefix(name, "📁 ") {
			t.Fatalf("a child took a slot a category header needed: %#v", got)
		}
	}
}

func TestTargetChoicesNeverEmitsANameOverDiscordsLimit(t *testing.T) {
	channels := []targetChannel{
		testCategory(10, strings.Repeat("c", 100), 0),
		testText(11, strings.Repeat("n", 100), 10, 0),
		testText(12, strings.Repeat("u", 100), 0, 0),
	}

	for _, c := range targetChoices(channels, "", testGuildID, false) {
		if n := utf8.RuneCountInString(c.ChoiceName()); n > maxChoiceName {
			t.Fatalf("a %d-rune name would make Discord reject the whole response: %#v", n, c.ChoiceName())
		}
	}
}

func TestChoiceNameClampsWithoutSplittingTheEmojiPrefix(t *testing.T) {
	got := choiceName("↳ 🔊 ", strings.Repeat("a", 100))

	if n := utf8.RuneCountInString(got); n != maxChoiceName {
		t.Fatalf("clamped to %d runes, want %d: %#v", n, maxChoiceName, got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("the clamp cut through a multi-byte rune: %#v", got)
	}
	if !strings.HasPrefix(got, "↳ 🔊 ") {
		t.Fatalf("the prefix did not survive the clamp: %#v", got)
	}
}

func TestChoiceNameLeavesANameInsideTheLimitAlone(t *testing.T) {
	got := choiceName("#", "general")

	if got != "#general" {
		t.Fatalf("a short name was rewritten: %#v", got)
	}
}

func TestChannelRowsReadsPositionAndParentFromDiscordChannels(t *testing.T) {
	payload := `[
		{"id":"10","type":4,"guild_id":"1","position":0,"name":"General"},
		{"id":"11","type":0,"guild_id":"1","position":3,"name":"chat","parent_id":"10"},
		{"id":"12","type":2,"guild_id":"1","position":1,"name":"Lounge"}
	]`

	var raw []discord.UnmarshalChannel
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		t.Fatalf("fixture did not decode: %v", err)
	}
	channels := make([]discord.GuildChannel, len(raw))
	for i := range raw {
		channels[i] = raw[i].Channel.(discord.GuildChannel)
	}

	got := channelRows(channels)
	want := []targetChannel{
		{ID: 10, Name: "General", Kind: discord.ChannelTypeGuildCategory, Position: 0},
		{ID: 11, Name: "chat", Kind: discord.ChannelTypeGuildText, ParentID: 10, Position: 3},
		{ID: 12, Name: "Lounge", Kind: discord.ChannelTypeGuildVoice, Position: 1},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the projection lost a field: %#v", got)
	}
}

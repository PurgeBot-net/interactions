package commands

import (
	"strings"
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"

	"github.com/PurgeBot-net/common/job"
	"github.com/PurgeBot-net/locale"
)

const (
	testGuildID snowflake.ID = 1
	testBotID   snowflake.ID = 2
	testRoleID  snowflake.ID = 3
)

// Refusing on more than the invite asks for would block bots added exactly as documented.
func TestRequiredPurgePermsMatchTheAdvertisedInvite(t *testing.T) {
	if requiredPurgePerms != 74752 {
		t.Fatalf("required set drifted from the advertised invite: %#v", requiredPurgePerms)
	}
}

func TestMissingForChannelReturnsZeroWhenFullyPermitted(t *testing.T) {
	if missing := missingForChannel(requiredPurgePerms); missing != 0 {
		t.Fatalf("a fully permitted channel must not be reported: %#v", missing)
	}
}

func TestMissingForChannelReportsOnlyViewChannelWhenViewIsDenied(t *testing.T) {
	have := discord.PermissionManageMessages | discord.PermissionReadMessageHistory

	missing := missingForChannel(have)
	if missing != discord.PermissionViewChannel {
		t.Fatalf("the other two are granted, so only View Channel is absent: %#v", missing)
	}
}

func TestMissingForChannelReportsEveryAbsentBit(t *testing.T) {
	have := discord.PermissionViewChannel

	missing := missingForChannel(have)
	if !missing.Has(discord.PermissionReadMessageHistory) || !missing.Has(discord.PermissionManageMessages) {
		t.Fatalf("both absent bits should be reported: %#v", missing)
	}
	if missing.Has(discord.PermissionViewChannel) {
		t.Fatalf("a granted permission leaked into the missing set: %#v", missing)
	}
}

func TestMissingForChannelReportsAllThreeWhenNothingIsGranted(t *testing.T) {
	missing := missingForChannel(0)
	if missing != requiredPurgePerms {
		t.Fatalf("a bot with no permissions must be told all three at once: %#v", missing)
	}
	if got := permissionNames(missing); got != "View Channel, Read Message History, Manage Messages" {
		t.Fatalf("all three must render, or the admin fixes them one retry at a time: %#v", got)
	}
}

func TestBotGuildPermissionsAdministratorFromEveryoneAloneGrantsAll(t *testing.T) {
	rolePerms := map[snowflake.ID]discord.Permissions{
		testGuildID: discord.PermissionAdministrator,
	}

	perms := botGuildPermissions(testGuildID, nil, rolePerms)
	if !perms.Has(requiredPurgePerms) {
		t.Fatalf("Administrator held through @everyone alone must still grant everything: %#v", perms)
	}
}

func TestBotChannelPermissionsAdministratorIgnoresChannelDenies(t *testing.T) {
	overwrites := discord.PermissionOverwrites{
		discord.RolePermissionOverwrite{RoleID: testGuildID, Deny: requiredPurgePerms},
	}

	perms := botChannelPermissions(discord.PermissionAdministrator, testGuildID, testBotID, nil, overwrites)
	if !perms.Has(requiredPurgePerms) {
		t.Fatalf("a channel overwrite must not be able to revoke Administrator: %#v", perms)
	}
}

// disgo's cache folds the two together, turning this exact setup into an allow.
func TestBotChannelPermissionsMemberDenyBeatsRoleAllow(t *testing.T) {
	overwrites := discord.PermissionOverwrites{
		discord.RolePermissionOverwrite{RoleID: testRoleID, Allow: discord.PermissionManageMessages},
		discord.MemberPermissionOverwrite{UserID: testBotID, Deny: discord.PermissionManageMessages},
	}

	perms := botChannelPermissions(requiredPurgePerms, testGuildID, testBotID, []snowflake.ID{testRoleID}, overwrites)
	if perms.Has(discord.PermissionManageMessages) {
		t.Fatalf("a member deny must survive a role allow: %#v", perms)
	}
	if missing := missingForChannel(perms); missing != discord.PermissionManageMessages {
		t.Fatalf("the denied bit should be the only one reported: %#v", missing)
	}
}

func TestBotChannelPermissionsWithNoOverwritesUsesGuildBase(t *testing.T) {
	perms := botChannelPermissions(requiredPurgePerms, testGuildID, testBotID, []snowflake.ID{testRoleID}, nil)
	if perms != requiredPurgePerms {
		t.Fatalf("a channel with no overwrites must keep the guild base unchanged: %#v", perms)
	}
}

func TestBotChannelPermissionsEveryoneOverwriteDenyIsApplied(t *testing.T) {
	overwrites := discord.PermissionOverwrites{
		discord.RolePermissionOverwrite{RoleID: testGuildID, Deny: discord.PermissionReadMessageHistory},
	}

	perms := botChannelPermissions(requiredPurgePerms, testGuildID, testBotID, nil, overwrites)
	if perms.Has(discord.PermissionReadMessageHistory) {
		t.Fatalf("the @everyone overwrite deny was not applied: %#v", perms)
	}
}

// Only a bit in both Allow and Deny distinguishes the two orderings.
func TestBotChannelPermissionsEveryoneOverwriteAppliesDenyThenAllow(t *testing.T) {
	overwrites := discord.PermissionOverwrites{
		discord.RolePermissionOverwrite{
			RoleID: testGuildID,
			Allow:  discord.PermissionManageMessages,
			Deny:   discord.PermissionManageMessages,
		},
	}

	perms := botChannelPermissions(requiredPurgePerms, testGuildID, testBotID, nil, overwrites)
	if !perms.Has(discord.PermissionManageMessages) {
		t.Fatalf("allow must be applied after deny on the @everyone overwrite: %#v", perms)
	}
}

func TestForumIsOutOfReachOnlyForViewChannelOnNonChannelTargets(t *testing.T) {
	forum := discord.ChannelTypeGuildForum
	text := discord.ChannelTypeGuildText

	if !forumIsOutOfReach(forum, job.TargetTypeServer, discord.PermissionViewChannel) {
		t.Fatalf("an unviewable forum contributes no channels, so it must not refuse a server purge")
	}
	if !forumIsOutOfReach(forum, job.TargetTypeServer, requiredPurgePerms) {
		t.Fatalf("a forum the bot cannot see at all still contributes no channels to the job")
	}
	if forumIsOutOfReach(forum, job.TargetTypeServer, discord.PermissionManageMessages) {
		t.Fatalf("a visible forum missing Manage Messages would fail in its threads, so it must refuse")
	}
	if forumIsOutOfReach(forum, job.TargetTypeChannel, discord.PermissionViewChannel) {
		t.Fatalf("a forum named directly as the target is resolved by the engine, so it must refuse")
	}
	if forumIsOutOfReach(text, job.TargetTypeServer, discord.PermissionViewChannel) {
		t.Fatalf("the exemption must not leak to ordinary channels")
	}
}

func TestRenderMissingPermsNamesTheChannelWhenOnlyOneIsDeficient(t *testing.T) {
	got := renderMissingPerms("en-GB", []channelDeficit{
		{ChannelID: 111, Missing: discord.PermissionViewChannel},
	}, 12)

	want := "I'm missing required permissions in: <#111>\n• View Channel"
	if got != want {
		t.Fatalf("one deficient channel must read as a single sentence naming it: %#v", got)
	}
}

func TestRenderMissingPermsNamesTheChannelForASingleChannelTarget(t *testing.T) {
	got := renderMissingPerms("en-GB", []channelDeficit{
		{ChannelID: 111, Missing: requiredPurgePerms},
	}, 1)

	want := "I'm missing required permissions in: <#111>\n• View Channel\n• Read Message History\n• Manage Messages"
	if got != want {
		t.Fatalf("the collapse must never swallow a single deficient channel: %#v", got)
	}
}

func TestRenderMissingPermsListsEachChannelWhenSeveralAreDeficient(t *testing.T) {
	got := renderMissingPerms("en-GB", []channelDeficit{
		{ChannelID: 111, Missing: discord.PermissionManageMessages},
		{ChannelID: 222, Missing: discord.PermissionManageMessages},
	}, 12)

	want := "I'm missing required permissions in: \n• <#111>: Manage Messages\n• <#222>: Manage Messages"
	if got != want {
		t.Fatalf("several deficient channels must render one per line: %#v", got)
	}
}

func TestRenderMissingPermsCollapsesWhenEveryChannelSharesTheSameGap(t *testing.T) {
	deficits := make([]channelDeficit, 0, 12)
	for i := range cap(deficits) {
		deficits = append(deficits, channelDeficit{
			ChannelID: snowflake.ID(100 + i), Missing: discord.PermissionManageMessages,
		})
	}

	got := renderMissingPerms("en-GB", deficits, len(deficits))
	if got != "I'm missing required permissions: Manage Messages" {
		t.Fatalf("naming twelve identically broken channels adds nothing: %#v", got)
	}
}

func TestRenderMissingPermsTruncatesAnOverlongList(t *testing.T) {
	deficits := make([]channelDeficit, 0, maxDeficitLines+5)
	for i := range cap(deficits) {
		deficits = append(deficits, channelDeficit{
			ChannelID: snowflake.ID(100 + i), Missing: discord.PermissionManageMessages,
		})
	}

	// Not every evaluated channel, so the collapse cannot fire and the cap has to.
	got := renderMissingPerms("en-GB", deficits, len(deficits)+1)
	if n := strings.Count(got, "• <#"); n != maxDeficitLines {
		t.Fatalf("channel rows were not capped: %#v", n)
	}
	if !strings.HasSuffix(got, "\n• ...") {
		t.Fatalf("truncation must be visible: %#v", got)
	}
}

// The space after the colon comes from the locale string and is invisible in Discord.
func TestPermissionLinesPutsEachMissingPermissionOnItsOwnLine(t *testing.T) {
	got := locale.MsgPurgeMissingPerms.In("en-GB", permissionLines(requiredPurgePerms))
	want := "I'm missing required permissions: \n• View Channel\n• Read Message History\n• Manage Messages"
	if got != want {
		t.Fatalf("the refusal is not rendered one permission per line: %#v", got)
	}
}

func TestPermissionLinesKeepsASingleMissingPermissionInline(t *testing.T) {
	if got := permissionLines(discord.PermissionManageMessages); got != "Manage Messages" {
		t.Fatalf("a lone permission must not become a one-item list: %#v", got)
	}
}

func TestPermissionNamesRendersInFixedOrder(t *testing.T) {
	got := permissionNames(requiredPurgePerms)
	if got != "View Channel, Read Message History, Manage Messages" {
		t.Fatalf("permission names are not in the fixed table order: %#v", got)
	}
}

func TestPermissionNamesOmitsBitsThatAreNotMissing(t *testing.T) {
	got := permissionNames(discord.PermissionManageMessages)
	if got != "Manage Messages" {
		t.Fatalf("only the set bits should render: %#v", got)
	}
}

package modules

import (
	"regexp"

	"github.com/pajbot/pajbot2/pkg"
)

func init() {
	Register("banned_names", func() pkg.ModuleSpec {
		badUsernames := []*regexp.Regexp{
			regexp.MustCompile(`tos_is_trash\d+`),
			regexp.MustCompile(`trash_is_the_tos\d+`),
			regexp.MustCompile(`terms_of_service_uncool\d+`),
			regexp.MustCompile(`tos_i_love_mods_no_toxic\d+`),
			regexp.MustCompile(`^kemper.+`),
			regexp.MustCompile(`^pudele\d+`),
			regexp.MustCompile(`^ninjal0ver\d+`),
			regexp.MustCompile(`^trihard_account_\d+`),
			regexp.MustCompile(`^h[il1]erot[il1]tan.+`),
		}

		return &moduleSpec{
			id:   "banned_names",
			name: "Banned names",
			maker: func(b base) pkg.Module {
				return newBannedNames(b, badUsernames)
			},

			enabledByDefault: false,
		}
	})
}

type bannedNames struct {
	base

	badUsernames []*regexp.Regexp
}

func newBannedNames(b base, badUsernames []*regexp.Regexp) pkg.Module {
	return &bannedNames{
		base: b,
	}
}

func (m bannedNames) OnMessage(bot pkg.BotChannel, user pkg.User, message pkg.Message, action pkg.Action) error {
	usernameBytes := []byte(user.GetName())
	for _, badUsername := range m.badUsernames {
		if badUsername.Match(usernameBytes) {
			action.Set(pkg.Ban{"Ban evasion"})
			return nil
		}
	}

	return nil
}

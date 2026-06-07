package main

import (
	"context"
	"fmt"

	"github.com/caseymrm/menuet/v2"
)

// RunNotifier consumes transitions from the poller and dispatches notifications
// per the user's settings. Spoiler rule: scores never appear in a notification
// body unless that specific game has been revealed.
func RunNotifier(ctx context.Context, cfg *Config, updates <-chan GameTransition) {
	for {
		select {
		case <-ctx.Done():
			return
		case t, ok := <-updates:
			if !ok {
				return
			}
			dispatch(cfg, t)
		}
	}
}

func dispatch(cfg *Config, t GameTransition) {
	a, b := t.Before, t.After
	revealed := cfg.Revealed(b)

	switch {
	case a.State == StateUpcoming && b.State == StateLive:
		if cfg.EffectiveNotify(b, PrefNotifyGameStart) {
			menuet.App().Notification(menuet.Notification{
				Title:      fmt.Sprintf("%s is starting", b.Matchup()),
				Message:    b.LeagueLabel,
				Identifier: "start-" + b.ID,
			})
		}
	case a.State == StateLive && b.State == StateFinal:
		if cfg.EffectiveNotify(b, PrefNotifyGameEnd) {
			n := menuet.Notification{
				Title:      fmt.Sprintf("%s is final", b.Matchup()),
				Identifier: "end-" + b.ID,
			}
			if revealed {
				n.Message = fmt.Sprintf("%s %d – %s %d",
					b.Away.Abbreviation, b.AwayScore,
					b.Home.Abbreviation, b.HomeScore)
			} else {
				n.Message = b.LeagueLabel
			}
			menuet.App().Notification(n)
		}
	case a.State == StateLive && b.State == StateLive && lead(a) != lead(b):
		if cfg.EffectiveNotify(b, PrefNotifyLeadChange) && revealed {
			menuet.App().Notification(menuet.Notification{
				Title: fmt.Sprintf("Lead change · %s", b.Matchup()),
				Message: fmt.Sprintf("%s %d – %s %d",
					b.Away.Abbreviation, b.AwayScore,
					b.Home.Abbreviation, b.HomeScore),
				Identifier: fmt.Sprintf("lead-%s-%d", b.ID, b.HomeScore+b.AwayScore),
			})
		}
	}
}

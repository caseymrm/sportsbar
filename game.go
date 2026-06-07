package main

import (
	"fmt"
	"sort"
	"time"
)

type GameState int

const (
	StateUpcoming GameState = iota
	StateLive
	StateFinal
)

type Game struct {
	ID          string
	LeagueKey   string
	LeagueLabel string
	Start       time.Time
	State       GameState

	Home, Away           EspnTeam
	HomeScore, AwayScore int
	Clock                string
	Period               int
	ShortDetail          string
	Links                map[string]string // rel -> URL (e.g. "summary", "boxscore")
}

func (g Game) InvolvesTeam(leagueKey, teamID string) bool {
	return g.LeagueKey == leagueKey && (g.Home.ID == teamID || g.Away.ID == teamID)
}

func (g Game) Matchup() string {
	return fmt.Sprintf("%s @ %s", g.Away.Abbreviation, g.Home.Abbreviation)
}

func (g Game) SummaryHidden(now time.Time) string {
	switch g.State {
	case StateUpcoming:
		return fmt.Sprintf("%s · %s", g.Matchup(), relativeFuture(g.Start, now))
	case StateLive:
		return fmt.Sprintf("%s · %s", g.Matchup(), pastWithVerb("started", g.Start, now))
	case StateFinal:
		return fmt.Sprintf("%s · %s", g.Matchup(), pastWithVerb("ended", g.endTimeEstimate(), now))
	}
	return g.Matchup()
}

func (g Game) SummaryRevealed(now time.Time) string {
	switch g.State {
	case StateUpcoming:
		return fmt.Sprintf("%s · %s", g.Matchup(), relativeFuture(g.Start, now))
	case StateLive:
		detail := g.ShortDetail
		if detail == "" {
			detail = fmt.Sprintf("Q%d %s", g.Period, g.Clock)
		}
		return fmt.Sprintf("%s %d  %s %d · %s",
			g.Away.Abbreviation, g.AwayScore,
			g.Home.Abbreviation, g.HomeScore, detail)
	case StateFinal:
		return fmt.Sprintf("%s %d  %s %d · %s",
			g.Away.Abbreviation, g.AwayScore,
			g.Home.Abbreviation, g.HomeScore,
			pastWithVerb("ended", g.endTimeEstimate(), now))
	}
	return g.Matchup()
}

// Winner reports the leading or winning team (Home or Away) and whether the
// result is decisive. For finals, decisive == true unless it's a true tie.
// For live games, decisive == true if anyone is ahead. Use the bool to decide
// whether bolding makes sense.
func (g Game) Winner() (team EspnTeam, score int, decisive bool) {
	if g.HomeScore > g.AwayScore {
		return g.Home, g.HomeScore, true
	}
	if g.AwayScore > g.HomeScore {
		return g.Away, g.AwayScore, true
	}
	return EspnTeam{}, 0, false
}

// OutcomeForTeam returns "W" if the given team won, "L" if they lost, "" if
// the game isn't final, is tied, or the team isn't in this game. Used to
// prefix recent-list labels with the favorite's result.
func (g Game) OutcomeForTeam(teamID string) string {
	if g.State != StateFinal || g.HomeScore == g.AwayScore {
		return ""
	}
	homeWon := g.HomeScore > g.AwayScore
	switch teamID {
	case g.Home.ID:
		if homeWon {
			return "W"
		}
		return "L"
	case g.Away.ID:
		if homeWon {
			return "L"
		}
		return "W"
	}
	return ""
}

// TitleSlot is the compact menubar-title form. favAbbr anchors which side
// of the matchup is the user's team. Live games are prefixed with "●" so the
// in-progress state is glanceable, not just inferable from the verb.
func (g Game) TitleSlot(favAbbr string, revealed bool, now time.Time) string {
	var body string
	if revealed && g.State != StateUpcoming {
		if favAbbr == g.Home.Abbreviation {
			body = fmt.Sprintf("%s %d-%d %s", g.Home.Abbreviation, g.HomeScore, g.AwayScore, g.Away.Abbreviation)
		} else {
			body = fmt.Sprintf("%s %d-%d %s", g.Away.Abbreviation, g.AwayScore, g.HomeScore, g.Home.Abbreviation)
		}
	} else {
		switch g.State {
		case StateUpcoming:
			body = fmt.Sprintf("%s · %s", favAbbr, relativeFutureShort(g.Start, now))
		case StateLive:
			body = fmt.Sprintf("%s · %s", favAbbr, pastWithVerb("live", g.Start, now))
		case StateFinal:
			body = fmt.Sprintf("%s · %s", favAbbr, pastWithVerb("ended", g.endTimeEstimate(), now))
		default:
			body = favAbbr
		}
	}
	if g.State == StateLive {
		return "● " + body
	}
	return body
}

// ESPN's basic scoreboard feed has no actual end timestamp. Approximating
// finals as "Start + 3h" is close enough for "finished 30m ago" labels while
// games are fresh; the staler the game, the looser the approximation.
func (g Game) endTimeEstimate() time.Time {
	return g.Start.Add(3 * time.Hour)
}

func SortRelevance(games []Game, now time.Time) {
	sort.Slice(games, func(i, j int) bool {
		return relevanceScore(games[i], now) < relevanceScore(games[j], now)
	})
}

func relevanceScore(g Game, now time.Time) float64 {
	switch g.State {
	case StateLive:
		return 0
	case StateUpcoming:
		hours := g.Start.Sub(now).Hours()
		if hours < 0 {
			hours = 0
		}
		return 1 + hours
	case StateFinal:
		hours := now.Sub(g.endTimeEstimate()).Hours()
		if hours < 0 {
			hours = 0
		}
		return 100 + hours
	}
	return 999
}

// relativeFuture formats a future time for use in "in 18h" / "on Tue" labels.
// Sub-day stays in "in Xm" / "in Xh" so the sentence reads naturally; ≥1 day
// switches to "on <weekday>" (within a week) or "on Jan 2" (beyond a week).
func relativeFuture(t, now time.Time) string {
	d := t.Sub(now)
	if d < time.Minute {
		return "starting"
	}
	if d < time.Hour {
		return fmt.Sprintf("in %dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("in %dh", int(d.Hours()))
	}
	if d < 7*24*time.Hour {
		return "on " + t.Format("Mon")
	}
	return "on " + t.Format("Jan 2")
}

// pastWithVerb formats a past time with progressive verbosity:
//
//	<1m:          "just now"           (verb dropped — context already clear)
//	<1h:          "ended 30m"
//	<1d:          "ended 3h"
//	<1wk:         "was Fri"            (verb dropped; "was" + weekday)
//	older:        "May 23rd"           (verb dropped; ordinal date)
//
// Used with verb="ended" for final games, "started" for live in the
// dropdown, and "live" for the menubar title.
func pastWithVerb(verb string, t, now time.Time) string {
	d := now.Sub(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%s %dm", verb, int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%s %dh", verb, int(d.Hours()))
	}
	if d < 7*24*time.Hour {
		return "was " + t.Format("Mon")
	}
	return t.Format("Jan ") + ordinalDay(t.Day())
}

// relativeFutureShort is the menubar form for upcoming games. Sub-day drops
// the "in" prefix; ≥1 day matches the long form.
func relativeFutureShort(t, now time.Time) string {
	d := t.Sub(now)
	if d < time.Minute {
		return "soon"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d < 7*24*time.Hour {
		return t.Format("Mon")
	}
	return t.Format("Jan 2")
}

// ordinalDay formats a day-of-month with an English ordinal suffix
// (1st, 2nd, 3rd, 4th-20th, 21st, 22nd, 23rd, 24th-30th, 31st).
func ordinalDay(d int) string {
	suffix := "th"
	if d < 11 || d > 13 {
		switch d % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", d, suffix)
}

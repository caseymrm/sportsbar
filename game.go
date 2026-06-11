package main

import (
	"fmt"
	"sort"
	"time"

	"github.com/caseymrm/menuet/v2"
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

// (OutcomeForTeam removed — the W/L marker now lives in the schedRow / title
// runs themselves rather than as a string prefix on the row label.)

// TitleRuns builds the menubar-title text runs for one game. Ports the
// state machine in design_handoff/variants-v2.jsx → titleStates. favTeamID
// anchors which side is "ours" so the runs can split into "our identity",
// "our score" (bold mono), and the opponent (secondary). revealed gates
// whether scores appear at all.
//
// State table (▸ marks run boundaries):
//
//	pregame        : GSW–MIN·semibold ▸  7:30pm·secondary
//	live·revealed  : ● ·red ▸ GSW ·semibold ▸ 71·bold-mono ▸ –68·sec-mono ▸  MIN·sec
//	live·hidden    : ● ·red ▸ GSW–MIN·semibold ▸  Q3·secondary
//	final·won      : W ·green·heavy ▸ GSW ·semibold ▸ 112·bold-mono ▸ –104·sec-mono ▸  MIN·sec
//	final·lost     : L ·red·heavy   ▸ GSW ·semibold ▸ 104·sec-mono  ▸ –112·bold-mono ▸  MIN·sec
//	final·hidden   : GSW–MIN·secondary ▸  ended 2h·tertiary
//
// "Our score leads" (bold), opponent's score takes the same secondary voice
// as the opponent abbreviation — quiet team, quiet number.
func (g Game) TitleRuns(favTeamID string, revealed bool, now time.Time) []menuet.TextRun {
	favAbbr, oppAbbr := favAndOpponentAbbr(g, favTeamID)

	switch g.State {
	case StateUpcoming:
		return []menuet.TextRun{
			r(favAbbr+"–"+oppAbbr, semibold),
			r(" "+relativeFutureShort(g.Start, now), sec),
		}

	case StateLive:
		if revealed {
			ourScore, theirScore := scoresFor(g, favTeamID)
			// Trailing team stays at primary color; only weight separates
			// the leader (Bold) from the trailer (Regular).
			return []menuet.TextRun{
				r("● ", red),
				r(favAbbr+" ", semibold),
				r(fmt.Sprintf("%d", ourScore), monoBold),
				r(fmt.Sprintf("–%d", theirScore), mono),
				r(" "+oppAbbr, plain),
			}
		}
		return []menuet.TextRun{
			r("● ", red),
			r(favAbbr+"–"+oppAbbr, semibold),
			r(" "+liveClock(g), sec),
		}

	case StateFinal:
		if !revealed {
			return []menuet.TextRun{
				r(favAbbr+"–"+oppAbbr, sec),
				r(" "+pastWithVerb("ended", g.endTimeEstimate(), now), ter),
			}
		}
		ourScore, theirScore := scoresFor(g, favTeamID)
		won := ourScore > theirScore
		// Only the winner gets a treatment — a subtle gold tint + WeightBold.
		// Loser side is plain default text, so there's no marker at all on a
		// loss (nothing punitive in the menubar). Identity weight on our
		// abbr stays Semibold either way.
		var ourColor, theirColor menuet.Color
		var ourWeight, theirWeight menuet.FontWeight
		if won {
			ourColor, ourWeight = titleGold, menuet.WeightBold
			theirColor, theirWeight = menuet.Color{}, menuet.WeightRegular
		} else {
			ourColor, ourWeight = menuet.Color{}, menuet.WeightRegular
			theirColor, theirWeight = titleGold, menuet.WeightBold
		}
		return []menuet.TextRun{
			r(favAbbr+" ", runOpts{color: ourColor, weight: menuet.WeightSemibold}),
			r(fmt.Sprintf("%d", ourScore), runOpts{color: ourColor, weight: ourWeight, mono: true}),
			r("–", runOpts{mono: true}),
			r(fmt.Sprintf("%d", theirScore), runOpts{color: theirColor, weight: theirWeight, mono: true}),
			r(" "+oppAbbr, runOpts{color: theirColor}),
		}
	}
	return []menuet.TextRun{r(favAbbr, runOpts{})}
}

// favAndOpponentAbbr returns the favorite team's abbreviation and the
// opponent's, in that order. If the favorite isn't in the game (shouldn't
// happen for games drawn from FavoriteGames) falls back to home/away.
func favAndOpponentAbbr(g Game, favTeamID string) (string, string) {
	if g.Home.ID == favTeamID {
		return g.Home.Abbreviation, g.Away.Abbreviation
	}
	if g.Away.ID == favTeamID {
		return g.Away.Abbreviation, g.Home.Abbreviation
	}
	return g.Home.Abbreviation, g.Away.Abbreviation
}

// scoresFor returns (ourScore, theirScore) given the favorite team.
func scoresFor(g Game, favTeamID string) (int, int) {
	if g.Home.ID == favTeamID {
		return g.HomeScore, g.AwayScore
	}
	return g.AwayScore, g.HomeScore
}

// liveClock returns a compact in-progress label like "Q3" or "5:42" — used
// only in the hidden-live menubar slot where we can't reveal the score but
// want to convey "this game is in progress, somewhere in here". Prefers a
// quarter/half label when available because it leaks less than displayClock.
func liveClock(g Game) string {
	if g.Period > 0 {
		return fmt.Sprintf("Q%d", g.Period)
	}
	if g.Clock != "" {
		return g.Clock
	}
	return "live"
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

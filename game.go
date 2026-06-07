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
		return fmt.Sprintf("%s · started %s", g.Matchup(), relativePast(g.Start, now))
	case StateFinal:
		return fmt.Sprintf("%s · finished %s", g.Matchup(), relativePast(g.endTimeEstimate(), now))
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
		return fmt.Sprintf("%s %d  %s %d · Final",
			g.Away.Abbreviation, g.AwayScore,
			g.Home.Abbreviation, g.HomeScore)
	}
	return g.Matchup()
}

// TitleSlot is the compact menubar-title form. favAbbr anchors which side
// of the matchup is the user's team.
func (g Game) TitleSlot(favAbbr string, revealed bool, now time.Time) string {
	if revealed && g.State != StateUpcoming {
		if favAbbr == g.Home.Abbreviation {
			return fmt.Sprintf("%s %d-%d %s", g.Home.Abbreviation, g.HomeScore, g.AwayScore, g.Away.Abbreviation)
		}
		return fmt.Sprintf("%s %d-%d %s", g.Away.Abbreviation, g.AwayScore, g.HomeScore, g.Home.Abbreviation)
	}
	switch g.State {
	case StateUpcoming:
		return fmt.Sprintf("%s · %s", favAbbr, relativeFutureShort(g.Start, now))
	case StateLive:
		return fmt.Sprintf("%s · live %s", favAbbr, relativePastShort(g.Start, now))
	case StateFinal:
		return fmt.Sprintf("%s · done %s", favAbbr, relativePastShort(g.endTimeEstimate(), now))
	}
	return favAbbr
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

// relativePast formats a past time for "finished 3h ago" / "finished Fri".
// Sub-day keeps the "Xh ago" suffix because dropping it ("finished 3h") would
// read badly; ≥1 day drops the suffix entirely since the weekday/date alone
// is unambiguous in the parent template.
func relativePast(t, now time.Time) string {
	d := now.Sub(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	if d < 7*24*time.Hour {
		return t.Format("Mon")
	}
	return t.Format("Jan 2")
}

// relativeFutureShort / relativePastShort are the menubar-title forms. Sub-day
// drops the "in"/"ago" prefix entirely; ≥1 day matches the long form.
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

func relativePastShort(t, now time.Time) string {
	d := now.Sub(t)
	if d < time.Minute {
		return "now"
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

package main

import (
	"log"
	"os"
	"time"
)

// fixtureMode reports whether the demo fixture should be installed.
//
// Snapshot mode implies fixture mode: the menuet-demo.json snapshot exists
// solely for the menuet.app showcase, which wants the curated menu, so a
// bare `make web-preview` must not silently capture cold-boot state (an
// easy mistake that guts the showcase mock). MENUET_DEMO_FIXTURE=0 opts
// out for the rare real-state snapshot; any other non-empty value forces
// the fixture in a normal (non-snapshot) run.
func fixtureMode() bool {
	switch os.Getenv("MENUET_DEMO_FIXTURE") {
	case "0":
		return false
	case "":
		return os.Getenv("MENUET_SNAPSHOT_PATH") != ""
	default:
		return true
	}
}

// installFixture overwrites the live config + poller with a curated set of
// Lakers and Dodgers games chosen to exercise every rich-text treatment:
//
//   - menubar title  : live·revealed Dodgers game (gold-underline winner)
//   - dropdown row 1 : the live Dodgers game with LIVE badge
//   - dropdown row 2 : yesterday's Lakers final (revealed, gold winner)
//   - Lakers submenu : mix of revealed wins, revealed losses, hidden ? rows,
//                      and upcoming games
//   - Dodgers submenu: same mix plus the live row pinned to the top of Recent
//
// The fixture skips persistence for everything it touches — favorites and
// reveal flags are mutated through the private maps directly so a snapshot
// run never writes to NSUserDefaults.
func installFixture(cfg *Config, p *Poller, m *Menu) {
	nba, _ := LeagueByKey("nba")
	mlb, _ := LeagueByKey("mlb")

	// Synchronously pull the team catalogs so we can attach real logos to
	// the fixture's EspnTeam values. NewMenu kicks these off as goroutines
	// for live use, but the snapshot can't wait on them.
	nbaTeams, err := FetchTeams(nba)
	if err != nil {
		log.Printf("fixture: nba teams: %v", err)
		return
	}
	mlbTeams, err := FetchTeams(mlb)
	if err != nil {
		log.Printf("fixture: mlb teams: %v", err)
		return
	}
	m.mu.Lock()
	m.teamCache["nba"] = nbaTeams
	m.teamCache["mlb"] = mlbTeams
	m.mu.Unlock()

	find := func(teams []EspnTeam, abbr string) EspnTeam {
		for _, t := range teams {
			if t.Abbreviation == abbr {
				return t
			}
		}
		// Synthetic fallback so the fixture still produces a valid row even
		// if ESPN renames an abbr we depend on.
		return EspnTeam{ID: "demo-" + abbr, Abbreviation: abbr, DisplayName: abbr}
	}

	lal := find(nbaTeams, "LAL")
	bos := find(nbaTeams, "BOS")
	den := find(nbaTeams, "DEN")
	phx := find(nbaTeams, "PHX")
	mil := find(nbaTeams, "MIL")
	lac := find(nbaTeams, "LAC")

	lad := find(mlbTeams, "LAD")
	nyy := find(mlbTeams, "NYY")
	chc := find(mlbTeams, "CHC")
	sf := find(mlbTeams, "SF")
	ari := find(mlbTeams, "ARI")
	_ = ari // reserved for a future Dodgers vs Arizona row if we expand

	now := time.Now()

	// Today's "FavoriteGames" slate — what the menubar title and the top of
	// the dropdown render from. Live Dodgers leads (sorted on top by
	// SortRelevance); the Lakers final shows under it.
	liveDodgers := Game{
		ID:          "demo-mlb-live",
		LeagueKey:   "mlb",
		LeagueLabel: "MLB",
		Start:       now.Add(-2 * time.Hour),
		State:       StateLive,
		Home:        lad,
		Away:        nyy,
		HomeScore:   5,
		AwayScore:   3,
		Period:      7,
		ShortDetail: "Bot 7th",
	}
	yesterdayLakers := Game{
		ID:          "demo-nba-final",
		LeagueKey:   "nba",
		LeagueLabel: "NBA",
		Start:       now.Add(-26 * time.Hour),
		State:       StateFinal,
		Home:        lal,
		Away:        bos,
		HomeScore:   118,
		AwayScore:   104,
		ShortDetail: "Final",
	}

	p.mu.Lock()
	p.games = map[string]Game{
		liveDodgers.ID:     liveDodgers,
		yesterdayLakers.ID: yesterdayLakers,
	}
	p.mu.Unlock()

	// Lakers schedule — recent W/L/hidden mix + a couple of upcoming games.
	lakersSched := []Game{
		yesterdayLakers, // gets deduped against today's FavoriteGames in the submenu
		{
			ID: "demo-lal-loss", LeagueKey: "nba", LeagueLabel: "NBA", State: StateFinal,
			Start: now.Add(-3 * 24 * time.Hour),
			Home:  den, Away: lal,
			HomeScore: 119, AwayScore: 105,
			ShortDetail: "Final",
		},
		{
			ID: "demo-lal-hidden", LeagueKey: "nba", LeagueLabel: "NBA", State: StateFinal,
			Start: now.Add(-5 * 24 * time.Hour),
			Home:  lal, Away: phx,
			HomeScore: 113, AwayScore: 102,
			ShortDetail: "Final",
		},
		{
			ID: "demo-lal-up1", LeagueKey: "nba", LeagueLabel: "NBA", State: StateUpcoming,
			Start: now.Add(2*24*time.Hour + 7*time.Hour),
			Home:  mil, Away: lal,
		},
		{
			ID: "demo-lal-up2", LeagueKey: "nba", LeagueLabel: "NBA", State: StateUpcoming,
			Start: now.Add(4*24*time.Hour + 4*time.Hour),
			Home:  lal, Away: lac,
		},
	}

	// Dodgers schedule — live game pinned, then sweep wins/losses + upcoming.
	dodgersSched := []Game{
		liveDodgers, // deduped against today's FavoriteGames
		{
			ID: "demo-lad-win", LeagueKey: "mlb", LeagueLabel: "MLB", State: StateFinal,
			Start: now.Add(-24 * time.Hour),
			Home:  lad, Away: chc,
			HomeScore: 5, AwayScore: 2,
			ShortDetail: "Final",
		},
		{
			ID: "demo-lad-loss", LeagueKey: "mlb", LeagueLabel: "MLB", State: StateFinal,
			Start: now.Add(-48 * time.Hour),
			Home:  chc, Away: lad,
			HomeScore: 7, AwayScore: 1,
			ShortDetail: "Final",
		},
		{
			ID: "demo-lad-hidden", LeagueKey: "mlb", LeagueLabel: "MLB", State: StateFinal,
			Start: now.Add(-3 * 24 * time.Hour),
			Home:  lad, Away: sf,
			HomeScore: 4, AwayScore: 2,
			ShortDetail: "Final",
		},
		{
			ID: "demo-lad-up1", LeagueKey: "mlb", LeagueLabel: "MLB", State: StateUpcoming,
			Start: now.Add(20 * time.Hour),
			Home:  sf, Away: lad,
		},
		{
			ID: "demo-lad-up2", LeagueKey: "mlb", LeagueLabel: "MLB", State: StateUpcoming,
			Start: now.Add(44 * time.Hour),
			Home:  sf, Away: lad,
		},
	}

	m.mu.Lock()
	m.scheduleCache["nba:"+lal.ID] = teamSchedule{games: lakersSched, fetched: now}
	m.scheduleCache["mlb:"+lad.ID] = teamSchedule{games: dodgersSched, fetched: now}
	m.mu.Unlock()

	// Favorites: Lakers + Dodgers, in that order so the dropdown lists them
	// alphabetically (LAL above LAD reads slightly nicer than the reverse).
	cfg.mu.Lock()
	cfg.favorites = []Favorite{
		{League: "nba", TeamID: lal.ID, Abbr: lal.Abbreviation, Name: lal.DisplayName},
		{League: "mlb", TeamID: lad.ID, Abbr: lad.Abbreviation, Name: lad.DisplayName},
	}
	// Reveal the marquee games so the gold treatment lands in the snapshot.
	// Hidden games (lal-hidden, lad-hidden) stay unrevealed so the ? veil
	// also appears.
	revealAt := now.Unix()
	for _, id := range []string{
		liveDodgers.ID,
		yesterdayLakers.ID,
		"demo-lad-win",
		"demo-lad-loss",
		"demo-lal-loss",
	} {
		cfg.revealed[id] = revealAt
	}
	cfg.mu.Unlock()
}

package main

import (
	"context"
	"log"
	"sync"
	"time"
)

// Poller drives ESPN scoreboard fetches on a per-league cadence derived from
// the current state of games involving favorite teams. One goroutine, one
// 5-second wakeup tick — leagues with no due fetch are skipped per-iteration.
//
// External signals:
//   - Refresh()      — request immediate fetch (menu open, favorite toggled).
//   - SetMenuOpen()  — switch to the menu-open fast cadence.
//   - Snapshot()     — read latest known games.
//   - Updates()      — receive state transitions for notification dispatch.
type Poller struct {
	cfg *Config

	mu       sync.RWMutex
	games    map[string]Game
	menuOpen bool

	nextFetch map[string]time.Time

	refreshCh chan struct{}
	updates   chan GameTransition
}

type GameTransition struct {
	Before, After Game
}

func NewPoller(cfg *Config) *Poller {
	return &Poller{
		cfg:       cfg,
		games:     make(map[string]Game),
		nextFetch: make(map[string]time.Time),
		refreshCh: make(chan struct{}, 1),
		updates:   make(chan GameTransition, 32),
	}
}

func (p *Poller) Updates() <-chan GameTransition { return p.updates }

func (p *Poller) Refresh() {
	select {
	case p.refreshCh <- struct{}{}:
	default:
	}
}

func (p *Poller) SetMenuOpen(open bool) {
	p.mu.Lock()
	p.menuOpen = open
	p.mu.Unlock()
	if open {
		p.Refresh()
	}
}

func (p *Poller) Snapshot() []Game {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Game, 0, len(p.games))
	for _, g := range p.games {
		out = append(out, g)
	}
	SortRelevance(out, time.Now())
	return out
}

func (p *Poller) FavoriteGames() []Game {
	favs := p.cfg.Favorites()
	if len(favs) == 0 {
		return nil
	}
	all := p.Snapshot()
	out := make([]Game, 0, len(favs))
	for _, g := range all {
		for _, f := range favs {
			if g.InvolvesTeam(f.League, f.TeamID) {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

func (p *Poller) Run(ctx context.Context) {
	defer close(p.updates)
	p.fetchDue(true)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.refreshCh:
			p.fetchDue(true)
		case <-ticker.C:
			p.fetchDue(false)
		}
	}
}

func (p *Poller) fetchDue(force bool) {
	favs := p.cfg.Favorites()
	if len(favs) == 0 {
		return
	}
	now := time.Now()
	for _, lkey := range uniqueLeagues(favs) {
		league, ok := LeagueByKey(lkey)
		if !ok {
			continue
		}
		p.mu.RLock()
		due := p.nextFetch[lkey]
		p.mu.RUnlock()
		if !force && !now.After(due) {
			continue
		}
		games, err := FetchScoreboard(league, time.Time{})
		if err != nil {
			log.Printf("scoreboard %s: %v", league.Key, err)
			p.mu.Lock()
			p.nextFetch[lkey] = now.Add(30 * time.Second)
			p.mu.Unlock()
			continue
		}
		p.merge(league.Key, games, favs)
		p.mu.Lock()
		p.nextFetch[lkey] = now.Add(p.intervalFor(league.Key, favs))
		p.mu.Unlock()
	}
}

// intervalFor picks the cadence for a league based on the current state of
// favorite-team games in it. Tightest applicable interval wins.
func (p *Poller) intervalFor(leagueKey string, favs []Favorite) time.Duration {
	// caller holds p.mu (called from fetchDue under Lock). Re-read fields
	// directly rather than taking the lock again.
	menuOpen := p.menuOpen
	now := time.Now()
	shortest := time.Hour
	for _, g := range p.games {
		if g.LeagueKey != leagueKey {
			continue
		}
		if !involvesAnyFavorite(g, favs) {
			continue
		}
		var d time.Duration
		switch g.State {
		case StateLive:
			if menuOpen {
				d = 5 * time.Second
			} else {
				d = 20 * time.Second
			}
		case StateUpcoming:
			until := g.Start.Sub(now)
			switch {
			case until < 5*time.Minute:
				d = 10 * time.Second
			case until < time.Hour:
				d = 2 * time.Minute
			case until < 6*time.Hour:
				d = 15 * time.Minute
			default:
				d = 30 * time.Minute
			}
		case StateFinal:
			age := now.Sub(g.endTimeEstimate())
			if age < 10*time.Minute {
				d = 30 * time.Second
			} else {
				d = time.Hour
			}
		}
		if d > 0 && d < shortest {
			shortest = d
		}
	}
	return shortest
}

func (p *Poller) merge(leagueKey string, fresh []Game, favs []Favorite) {
	p.mu.Lock()
	defer p.mu.Unlock()
	seen := make(map[string]bool, len(fresh))
	for _, g := range fresh {
		seen[g.ID] = true
		old, hadOld := p.games[g.ID]
		p.games[g.ID] = g
		if !involvesAnyFavorite(g, favs) {
			continue
		}
		if !hadOld {
			continue
		}
		if isTransition(old, g) {
			select {
			case p.updates <- GameTransition{Before: old, After: g}:
			default:
				log.Printf("updates channel full, dropping transition for %s", g.ID)
			}
		}
	}
	for id, g := range p.games {
		if g.LeagueKey != leagueKey {
			continue
		}
		if !seen[id] {
			delete(p.games, id)
		}
	}
}

func involvesAnyFavorite(g Game, favs []Favorite) bool {
	for _, f := range favs {
		if g.InvolvesTeam(f.League, f.TeamID) {
			return true
		}
	}
	return false
}

// isTransition reports whether a state change is worth surfacing as an event.
func isTransition(a, b Game) bool {
	if a.State != b.State {
		return true
	}
	if b.State == StateLive && lead(a) != lead(b) {
		return true
	}
	return false
}

func lead(g Game) int {
	switch {
	case g.HomeScore > g.AwayScore:
		return 1
	case g.HomeScore < g.AwayScore:
		return -1
	}
	return 0
}

func uniqueLeagues(favs []Favorite) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, f := range favs {
		if !seen[f.League] {
			seen[f.League] = true
			out = append(out, f.League)
		}
	}
	return out
}

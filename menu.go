package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caseymrm/menuet"
)

// Menu owns the menubar UI. Title rendering and dropdown construction read
// from the Poller and Config; click handlers write to Config and trigger a
// menu/title refresh.
type Menu struct {
	cfg *Config
	p   *Poller

	mu            sync.Mutex
	teamCache     map[string][]EspnTeam
	teamCacheLoad map[string]bool
}

func NewMenu(cfg *Config, p *Poller) *Menu {
	return &Menu{
		cfg:           cfg,
		p:             p,
		teamCache:     make(map[string][]EspnTeam),
		teamCacheLoad: make(map[string]bool),
	}
}

func (m *Menu) Title() *menuet.MenuState {
	games := m.p.FavoriteGames()
	if len(games) == 0 {
		return &menuet.MenuState{Title: "sportsbar"}
	}
	now := time.Now()
	favs := m.cfg.Favorites()
	parts := make([]string, 0, len(games))
	for _, g := range games {
		abbr := favoriteAbbrInGame(g, favs)
		revealed := m.cfg.Revealed(g.ID)
		parts = append(parts, g.TitleSlot(abbr, revealed, now))
	}
	return &menuet.MenuState{Title: strings.Join(parts, " | ")}
}

func favoriteAbbrInGame(g Game, favs []Favorite) string {
	for _, f := range favs {
		if f.League == g.LeagueKey {
			if f.TeamID == g.Home.ID {
				return g.Home.Abbreviation
			}
			if f.TeamID == g.Away.ID {
				return g.Away.Abbreviation
			}
		}
	}
	return g.Home.Abbreviation
}

func (m *Menu) Children() []menuet.MenuItem {
	favs := m.cfg.Favorites()
	now := time.Now()
	items := []menuet.MenuItem{}

	if len(favs) == 0 {
		// Empty state: skip the wrapping "Choose teams" submenu and put the
		// per-league pickers directly at the top so adding a first team takes
		// one fewer click.
		items = append(items, menuet.MenuItem{
			Text:       "Pick a team to follow:",
			FontWeight: menuet.WeightBold,
		})
		for _, league := range Leagues {
			l := league
			items = append(items, menuet.MenuItem{
				Text:     l.Label,
				Children: func() []menuet.MenuItem { return m.teamSubmenu(l) },
			})
		}
	} else {
		games := m.p.FavoriteGames()
		if len(games) == 0 {
			items = append(items, menuet.MenuItem{
				Text: "No games for your teams today",
			})
		} else {
			for _, g := range games {
				items = append(items, m.gameItem(g, now))
			}
		}
		items = append(items, menuet.MenuItem{Type: menuet.Separator})
		items = append(items, menuet.MenuItem{
			Text:     fmt.Sprintf("My teams (%d/%d)", len(favs), MaxFavorites),
			Children: m.myTeamsSubmenu,
		})
	}

	items = append(items, menuet.MenuItem{Type: menuet.Separator})
	items = append(items, menuet.MenuItem{
		Text:     "Settings",
		Children: m.settingsSubmenu,
	})
	return items
}

func (m *Menu) gameItem(g Game, now time.Time) menuet.MenuItem {
	revealed := m.cfg.Revealed(g.ID)
	var label string
	if revealed {
		label = g.SummaryRevealed(now)
	} else {
		label = g.SummaryHidden(now)
	}
	gid := g.ID
	return menuet.MenuItem{
		Text:     fmt.Sprintf("%s · %s", g.LeagueLabel, label),
		Children: func() []menuet.MenuItem { return m.gameSubmenu(gid) },
	}
}

func (m *Menu) gameSubmenu(gameID string) []menuet.MenuItem {
	// Re-resolve current game state at submenu open so we reflect fresh data
	// rather than what the parent menu was built from.
	snap := m.p.Snapshot()
	var g Game
	found := false
	for _, s := range snap {
		if s.ID == gameID {
			g = s
			found = true
			break
		}
	}
	if !found {
		return []menuet.MenuItem{{Text: "Game no longer available"}}
	}
	now := time.Now()
	revealed := m.cfg.Revealed(g.ID)

	items := []menuet.MenuItem{
		{Text: g.Matchup(), FontWeight: menuet.WeightBold},
	}
	if g.ShortDetail != "" {
		items = append(items, menuet.MenuItem{Text: g.ShortDetail})
	}
	if revealed {
		items = append(items, menuet.MenuItem{Text: g.SummaryRevealed(now)})
		items = append(items, menuet.MenuItem{Type: menuet.Separator})
		items = append(items, menuet.MenuItem{
			Text: "Hide scores",
			Clicked: func() {
				m.cfg.Hide(g.ID)
				menuet.App().MenuChanged()
				m.refreshTitle()
			},
		})
	} else if g.State != StateUpcoming {
		items = append(items, menuet.MenuItem{Text: g.SummaryHidden(now)})
		items = append(items, menuet.MenuItem{Type: menuet.Separator})
		items = append(items, menuet.MenuItem{
			Text: "Show scores",
			Clicked: func() {
				m.cfg.Reveal(g.ID)
				menuet.App().MenuChanged()
				m.refreshTitle()
			},
		})
	} else {
		items = append(items, menuet.MenuItem{Text: g.SummaryHidden(now)})
	}
	return items
}

func (m *Menu) refreshTitle() {
	menuet.App().SetMenuState(m.Title())
}

func (m *Menu) myTeamsSubmenu() []menuet.MenuItem {
	favs := m.cfg.Favorites()
	items := []menuet.MenuItem{}
	if len(favs) > 0 {
		items = append(items, menuet.MenuItem{
			Text:       "Following (click to remove):",
			FontWeight: menuet.WeightSemibold,
		})
		for _, f := range favs {
			f := f
			items = append(items, menuet.MenuItem{
				Text:  fmt.Sprintf("%s (%s)", f.Name, leagueLabel(f.League)),
				State: true,
				Clicked: func() {
					m.cfg.ToggleFavorite(f)
					m.p.Refresh()
					menuet.App().MenuChanged()
					m.refreshTitle()
				},
			})
		}
		items = append(items, menuet.MenuItem{Type: menuet.Separator})
	}
	if len(favs) < MaxFavorites {
		items = append(items, menuet.MenuItem{
			Text:       "Add another team:",
			FontWeight: menuet.WeightSemibold,
		})
		for _, league := range Leagues {
			l := league
			items = append(items, menuet.MenuItem{
				Text:     l.Label,
				Children: func() []menuet.MenuItem { return m.teamSubmenu(l) },
			})
		}
	} else {
		items = append(items, menuet.MenuItem{
			Text: fmt.Sprintf("At max (%d teams) — remove one to add another", MaxFavorites),
		})
	}
	return items
}

func leagueLabel(key string) string {
	if l, ok := LeagueByKey(key); ok {
		return l.Label
	}
	return key
}

func (m *Menu) teamSubmenu(league League) []menuet.MenuItem {
	teams := m.teams(league)
	if len(teams) == 0 {
		return []menuet.MenuItem{{Text: "Loading teams…"}}
	}
	sort.Slice(teams, func(i, j int) bool { return teams[i].DisplayName < teams[j].DisplayName })
	items := make([]menuet.MenuItem, 0, len(teams))
	for _, t := range teams {
		t := t
		fav := m.cfg.IsFavorite(league.Key, t.ID)
		items = append(items, menuet.MenuItem{
			Text:  t.DisplayName,
			State: fav,
			Clicked: func() {
				m.cfg.ToggleFavorite(Favorite{
					League: league.Key,
					TeamID: t.ID,
					Abbr:   t.Abbreviation,
					Name:   t.DisplayName,
				})
				m.p.Refresh()
				menuet.App().MenuChanged()
				m.refreshTitle()
			},
		})
	}
	return items
}

// teams returns the cached team catalog for a league, kicking off a fetch in
// the background on first access (and on retry after a failure).
func (m *Menu) teams(league League) []EspnTeam {
	m.mu.Lock()
	cached, ok := m.teamCache[league.Key]
	loading := m.teamCacheLoad[league.Key]
	m.mu.Unlock()
	if ok && len(cached) > 0 {
		return cached
	}
	if loading {
		return cached
	}
	m.mu.Lock()
	m.teamCacheLoad[league.Key] = true
	m.mu.Unlock()
	go func() {
		teams, err := FetchTeams(league)
		if err != nil {
			m.mu.Lock()
			m.teamCacheLoad[league.Key] = false
			m.mu.Unlock()
			return
		}
		m.mu.Lock()
		m.teamCache[league.Key] = teams
		m.mu.Unlock()
		menuet.App().MenuChanged()
	}()
	return cached
}

func (m *Menu) settingsSubmenu() []menuet.MenuItem {
	return []menuet.MenuItem{
		{
			Text:  "Show scores by default",
			State: m.cfg.ScoresByDefault(),
			Clicked: func() {
				m.cfg.SetScoresByDefault(!m.cfg.ScoresByDefault())
				menuet.App().MenuChanged()
				m.refreshTitle()
			},
		},
		{Type: menuet.Separator},
		{
			Text:  "Notify when game starts",
			State: m.cfg.NotifyGameStart(),
			Clicked: func() {
				m.cfg.SetNotifyGameStart(!m.cfg.NotifyGameStart())
				menuet.App().MenuChanged()
			},
		},
		{
			Text:  "Notify when game ends",
			State: m.cfg.NotifyGameEnd(),
			Clicked: func() {
				m.cfg.SetNotifyGameEnd(!m.cfg.NotifyGameEnd())
				menuet.App().MenuChanged()
			},
		},
		{
			Text:  "Notify on lead change (revealed games only)",
			State: m.cfg.NotifyLeadChange(),
			Clicked: func() {
				m.cfg.SetNotifyLeadChange(!m.cfg.NotifyLeadChange())
				menuet.App().MenuChanged()
			},
		},
	}
}

package main

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caseymrm/menuet/v2"
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

	scheduleCache    map[string]teamSchedule // key: league:teamID
	scheduleCacheTTL time.Duration

	logos *LogoServer
}

type teamSchedule struct {
	games   []Game
	fetched time.Time
	loading bool
}

func NewMenu(cfg *Config, p *Poller) *Menu {
	m := &Menu{
		cfg:              cfg,
		p:                p,
		teamCache:        make(map[string][]EspnTeam),
		teamCacheLoad:    make(map[string]bool),
		scheduleCache:    make(map[string]teamSchedule),
		scheduleCacheTTL: time.Hour,
	}
	m.logos = NewLogoServer(m.teams)
	// Eagerly prime team catalogs for leagues with favorites so logos render
	// on first menu open instead of waiting until the user opens the picker.
	for _, f := range cfg.Favorites() {
		if l, ok := LeagueByKey(f.League); ok {
			m.teams(l)
		}
	}
	return m
}

// favoriteLogoURL returns the default (transparent-background) logo URL for
// the menubar status item, where macOS applies a template mask anyway.
func (m *Menu) favoriteLogoURL(f Favorite) string {
	return m.favoriteLogo(f, false)
}

// favoriteLogoForMenuURL returns a logo URL suitable for dropdown items.
// Goes through the local composite server which puts the team logo on a
// white disc — that's the only way to guarantee readability against
// translucent system menus, which can pick up any color from the wallpaper
// behind them. If the server failed to bind, falls back to ESPN's "dark"
// variant in dark mode or the default logo in light mode.
func (m *Menu) favoriteLogoForMenuURL(f Favorite) string {
	if u := m.logos.URL(f.League, f.TeamID); u != "" {
		return u
	}
	return m.favoriteLogo(f, isDarkMode())
}

func (m *Menu) favoriteLogo(f Favorite, dark bool) string {
	league, ok := LeagueByKey(f.League)
	if !ok {
		return ""
	}
	for _, t := range m.teams(league) {
		if t.ID == f.TeamID {
			if dark {
				return t.LogoDark()
			}
			return t.Logo()
		}
	}
	return ""
}

// isDarkMode reports whether the user has macOS Dark Appearance active.
// `defaults read -g AppleInterfaceStyle` returns "Dark" in dark mode and
// errors (key not present) in light mode. Cached on first call since toggling
// theme mid-session is rare and a stale read just shows a slightly-wrong
// logo variant until app restart.
var darkModeOnce sync.Once
var darkModeCached bool

func isDarkMode() bool {
	darkModeOnce.Do(func() {
		out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
		if err == nil && strings.TrimSpace(string(out)) == "Dark" {
			darkModeCached = true
		}
	})
	return darkModeCached
}

func (m *Menu) Title() *menuet.MenuState {
	games := m.p.FavoriteGames()
	favs := m.cfg.Favorites()
	if len(games) == 0 {
		// No games today: show a logo for the first favorite if we have one,
		// otherwise plain "sportsbar" text.
		state := &menuet.MenuState{Title: "sportsbar"}
		if len(favs) > 0 {
			state.Image = m.favoriteLogoURL(favs[0])
		}
		return state
	}
	now := time.Now()
	parts := make([]string, 0, len(games))
	for _, g := range games {
		abbr := favoriteAbbrInGame(g, favs)
		revealed := m.cfg.Revealed(g)
		parts = append(parts, g.TitleSlot(abbr, revealed, now))
	}
	// The first game is the most relevant (FavoriteGames is pre-sorted) —
	// surface its favorite team's logo in the menubar's single image slot.
	state := &menuet.MenuState{Title: strings.Join(parts, " | ")}
	if topFav, ok := favoriteInGame(games[0], favs); ok {
		state.Image = m.favoriteLogoURL(topFav)
	}
	return state
}

func favoriteInGame(g Game, favs []Favorite) (Favorite, bool) {
	for _, f := range favs {
		if g.InvolvesTeam(f.League, f.TeamID) {
			return f, true
		}
	}
	return Favorite{}, false
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
		items = append(items, labelItem("Pick a team to follow:", menuet.WeightBold))
		enabled := m.enabledLeagueList()
		if len(enabled) == 0 {
			items = append(items, labelItem("No leagues enabled — see Settings → Leagues", menuet.WeightRegular))
		} else {
			for _, league := range enabled {
				l := league
				items = append(items, menuet.Regular{
					Text:     l.Label,
					Children: func() []menuet.MenuItem { return m.teamSubmenu(l) },
				})
			}
		}
	} else {
		games := m.p.FavoriteGames()
		if len(games) == 0 {
			items = append(items, labelItem("No games for your teams today", menuet.WeightRegular))
		} else {
			for _, g := range games {
				items = append(items, m.gameItem(g, now))
			}
		}
		items = append(items, menuet.Separator{})
		for _, f := range favs {
			f := f
			items = append(items, menuet.Regular{
				Text:     fmt.Sprintf("%s (%s)", f.Name, leagueLabel(f.League)),
				Image:    m.favoriteLogoForMenuURL(f),
				Children: func() []menuet.MenuItem { return m.favoriteTeamSubmenu(f) },
			})
		}
		if len(favs) < MaxFavorites {
			items = append(items, menuet.Regular{
				Text:     "Add another favorite",
				Children: m.addAnotherSubmenu,
			})
		}
	}

	items = append(items, menuet.Separator{})
	items = append(items, menuet.Regular{
		Text:     "Settings",
		Children: m.settingsSubmenu,
	})
	return items
}

func (m *Menu) gameItem(g Game, now time.Time) menuet.MenuItem {
	revealed := m.cfg.Revealed(g)
	var label string
	if revealed {
		label = g.SummaryRevealed(now)
		// Prefix the favorite team's W/L outcome for finals. For a game between
		// two favorites we pick the first that matches — arbitrary but stable.
		for _, f := range m.cfg.Favorites() {
			if g.InvolvesTeam(f.League, f.TeamID) {
				if o := g.OutcomeForTeam(f.TeamID); o != "" {
					label = o + " · " + label
				}
				break
			}
		}
	} else {
		label = g.SummaryHidden(now)
	}
	gid := g.ID
	return menuet.Regular{
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
		return []menuet.MenuItem{labelItem("Game no longer available", menuet.WeightRegular)}
	}
	now := time.Now()
	revealed := m.cfg.Revealed(g)

	items := []menuet.MenuItem{
		labelItem(g.Matchup(), menuet.WeightBold),
	}
	if revealed {
		items = append(items, scoreLines(g)...)
		items = append(items, labelItem(gameTimeLabel(g, now), menuet.WeightRegular))
		items = append(items, menuet.Separator{})
		items = append(items, menuet.Regular{
			Text: "Hide scores",
			Clicked: func() {
				m.cfg.Hide(g.ID)
				menuet.App().MenuChanged()
				m.refreshTitle()
			},
		})
	} else if g.State != StateUpcoming {
		items = append(items, labelItem(g.SummaryHidden(now), menuet.WeightRegular))
		items = append(items, menuet.Separator{})
		items = append(items, menuet.Regular{
			Text: "Show scores",
			Clicked: func() {
				m.cfg.Reveal(g.ID)
				menuet.App().MenuChanged()
				m.refreshTitle()
			},
		})
	} else {
		items = append(items, labelItem(g.SummaryHidden(now), menuet.WeightRegular))
	}
	items = appendOpenInESPN(items, g)
	return items
}

// gameTimeLabel produces the time-context line shown in a game submenu:
// "Q3 5:42" for live, "ended Mon" for finals, "on Tue" for upcoming.
func gameTimeLabel(g Game, now time.Time) string {
	switch g.State {
	case StateLive:
		if g.ShortDetail != "" {
			return g.ShortDetail
		}
		return fmt.Sprintf("Q%d %s", g.Period, g.Clock)
	case StateFinal:
		return pastWithVerb("ended", g.endTimeEstimate(), now)
	case StateUpcoming:
		return relativeFuture(g.Start, now)
	}
	return ""
}

// appendOpenInESPN adds a separator + "Open in ESPN" item if a Gamecast link
// is present. Skipped silently if the API didn't include links for this game.
func appendOpenInESPN(items []menuet.MenuItem, g Game) []menuet.MenuItem {
	url := g.Links["summary"]
	if url == "" {
		return items
	}
	items = append(items, menuet.Separator{})
	items = append(items, menuet.Regular{
		Text: "Open in ESPN",
		Clicked: func() {
			_ = exec.Command("open", url).Start()
		},
	})
	return items
}

func (m *Menu) refreshTitle() {
	menuet.App().SetMenuState(m.Title())
}

// labelItem renders informational text that should look enabled (not greyed).
// menuet.m:109 sets item.enabled = clickable || hasChildren — a no-op Clicked
// is the simplest way to force the enabled rendering without changing menuet.
func labelItem(text string, weight menuet.FontWeight) menuet.MenuItem {
	return menuet.Regular{
		Text:       text,
		FontWeight: weight,
		Clicked:    func() {},
	}
}

// scoreLines renders one labelItem per team (away first, then home) with the
// winner / current leader's line bolded. menuet supports only one FontWeight
// per item so we get bold-on-winner by separating teams onto their own lines.
func scoreLines(g Game) []menuet.MenuItem {
	winner, _, decisive := g.Winner()
	weightFor := func(t EspnTeam) menuet.FontWeight {
		if decisive && t.ID == winner.ID {
			return menuet.WeightBold
		}
		return menuet.WeightRegular
	}
	return []menuet.MenuItem{
		labelItem(fmt.Sprintf("%s %d", g.Away.Abbreviation, g.AwayScore), weightFor(g.Away)),
		labelItem(fmt.Sprintf("%s %d", g.Home.Abbreviation, g.HomeScore), weightFor(g.Home)),
	}
}

func (m *Menu) addAnotherSubmenu() []menuet.MenuItem {
	enabled := m.enabledLeagueList()
	if len(enabled) == 0 {
		return []menuet.MenuItem{
			labelItem("No leagues enabled — see Settings → Leagues", menuet.WeightRegular),
		}
	}
	items := make([]menuet.MenuItem, 0, len(enabled))
	for _, league := range enabled {
		l := league
		items = append(items, menuet.Regular{
			Text:     l.Label,
			Children: func() []menuet.MenuItem { return m.teamSubmenu(l) },
		})
	}
	return items
}

// enabledLeagueList returns the league objects currently enabled by the user,
// in the canonical Leagues declaration order (so US Pro stays first, etc.).
func (m *Menu) enabledLeagueList() []League {
	out := make([]League, 0, len(Leagues))
	for _, l := range Leagues {
		if m.cfg.IsLeagueEnabled(l.Key) {
			out = append(out, l)
		}
	}
	return out
}

// scheduleWindow is how many recent / upcoming games to show per team.
const scheduleWindow = 5

func (m *Menu) favoriteTeamSubmenu(f Favorite) []menuet.MenuItem {
	league, ok := LeagueByKey(f.League)
	if !ok {
		return []menuet.MenuItem{menuet.Regular{Text: "Unknown league"}}
	}
	sched, fresh := m.teamSchedule(league, f.TeamID)

	// Exclude games already shown at the top of the menu (live / today / just
	// finished) so we don't repeat them.
	visible := map[string]bool{}
	for _, g := range m.p.FavoriteGames() {
		visible[g.ID] = true
	}

	now := time.Now()
	recent := []Game{}
	upcoming := []Game{}
	for _, g := range sched {
		if visible[g.ID] {
			continue
		}
		switch g.State {
		case StateFinal:
			recent = append(recent, g)
		case StateUpcoming, StateLive:
			upcoming = append(upcoming, g)
		}
	}
	// Recent: most-recent first, capped.
	sort.Slice(recent, func(i, j int) bool { return recent[i].Start.After(recent[j].Start) })
	if len(recent) > scheduleWindow {
		recent = recent[:scheduleWindow]
	}
	// Upcoming: soonest first, capped.
	sort.Slice(upcoming, func(i, j int) bool { return upcoming[i].Start.Before(upcoming[j].Start) })
	if len(upcoming) > scheduleWindow {
		upcoming = upcoming[:scheduleWindow]
	}

	items := []menuet.MenuItem{}
	if !fresh && len(sched) == 0 {
		items = append(items, labelItem("Loading schedule…", menuet.WeightRegular))
	}
	if len(recent) > 0 {
		items = append(items, menuet.Regular{Text: "Recent", FontWeight: menuet.WeightSemibold})
		for _, g := range recent {
			items = append(items, m.scheduleGameItem(g, now, f.TeamID))
		}
	}
	if len(upcoming) > 0 {
		items = append(items, menuet.Regular{Text: "Upcoming", FontWeight: menuet.WeightSemibold})
		for _, g := range upcoming {
			items = append(items, m.scheduleGameItem(g, now, f.TeamID))
		}
	}
	if len(recent) == 0 && len(upcoming) == 0 && fresh {
		items = append(items, labelItem("No other games on this team's schedule", menuet.WeightRegular))
	}

	// Collect IDs of currently-visible games (recent + upcoming with scores —
	// upcoming games have no score so they're ignored). If any aren't already
	// revealed (via per-game flag, per-team default, or global default), offer
	// a bulk "Show all scores".
	revealable := []Game{}
	for _, g := range recent {
		revealable = append(revealable, g)
	}
	hasHidden := false
	for _, g := range revealable {
		if !m.cfg.Revealed(g) {
			hasHidden = true
			break
		}
	}
	if hasHidden {
		ids := make([]string, 0, len(revealable))
		for _, g := range revealable {
			ids = append(ids, g.ID)
		}
		items = append(items, menuet.Separator{})
		items = append(items, menuet.Regular{
			Text: "Show all scores",
			Clicked: func() {
				for _, id := range ids {
					m.cfg.Reveal(id)
				}
				menuet.App().MenuChanged()
				m.refreshTitle()
			},
		})
	}

	items = append(items, menuet.Separator{})
	items = append(items, menuet.Regular{
		Text:     "Team settings",
		Children: func() []menuet.MenuItem { return m.teamSettingsSubmenu(f) },
	})
	items = append(items, menuet.Regular{
		Text: "Remove from favorites",
		Clicked: func() {
			m.cfg.ToggleFavorite(f)
			m.p.Refresh()
			menuet.App().MenuChanged()
			m.refreshTitle()
		},
	})
	return items
}

// teamSettingsSubmenu builds the per-team overrides menu. Each setting is a
// sub-submenu with three choices: use global / always on / always off.
func (m *Menu) teamSettingsSubmenu(f Favorite) []menuet.MenuItem {
	items := []menuet.MenuItem{}
	add := func(label string, field PrefField, globalVal bool) {
		items = append(items, menuet.Regular{
			Text:     prefHeader(label, m.cfg.TeamPref(f.League, f.TeamID, field), globalVal),
			Children: func() []menuet.MenuItem { return m.prefChoiceSubmenu(f, field, globalVal) },
		})
	}
	add("Show scores by default", PrefScoresByDefault, m.cfg.ScoresByDefault())
	add("Notify when game starts", PrefNotifyGameStart, m.cfg.NotifyGameStart())
	add("Notify when game ends", PrefNotifyGameEnd, m.cfg.NotifyGameEnd())
	add("Notify on lead change", PrefNotifyLeadChange, m.cfg.NotifyLeadChange())
	return items
}

// prefHeader produces the parent-item label, showing the effective state and
// whether it's overridden ("override: on") or following the global ("default: off").
func prefHeader(label string, pref *bool, globalVal bool) string {
	if pref == nil {
		return fmt.Sprintf("%s — default (%s)", label, onOff(globalVal))
	}
	return fmt.Sprintf("%s — override: %s", label, onOff(*pref))
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// prefChoiceSubmenu shows the three options for a single per-team setting,
// with the current state checkmarked.
func (m *Menu) prefChoiceSubmenu(f Favorite, field PrefField, globalVal bool) []menuet.MenuItem {
	current := m.cfg.TeamPref(f.League, f.TeamID, field)
	set := func(v *bool) func() {
		return func() {
			m.cfg.SetTeamPref(f.League, f.TeamID, field, v)
			menuet.App().MenuChanged()
			m.refreshTitle()
		}
	}
	on, off := true, false
	return []menuet.MenuItem{
		menuet.Regular{
			Text:    fmt.Sprintf("Use global default (%s)", onOff(globalVal)),
			State:   current == nil,
			Clicked: set(nil),
		},
		menuet.Regular{
			Text:    "Always on for this team",
			State:   current != nil && *current,
			Clicked: set(&on),
		},
		menuet.Regular{
			Text:    "Always off for this team",
			State:   current != nil && !*current,
			Clicked: set(&off),
		},
	}
}

// scheduleGameItem renders a schedule entry with the same spoiler-aware
// formatting as a top-level game. Final scores hide unless revealed; upcoming
// games have no score so show plain. When revealed, the favorite team's W/L
// outcome is prefixed.
func (m *Menu) scheduleGameItem(g Game, now time.Time, favTeamID string) menuet.MenuItem {
	revealed := m.cfg.Revealed(g)
	var label string
	if revealed {
		label = g.SummaryRevealed(now)
		if o := g.OutcomeForTeam(favTeamID); o != "" {
			label = o + " · " + label
		}
	} else {
		label = g.SummaryHidden(now)
	}
	gid := g.ID
	return menuet.Regular{
		Text:     label,
		Children: func() []menuet.MenuItem { return m.scheduleGameSubmenu(gid) },
	}
}

// scheduleGameSubmenu mirrors gameSubmenu but resolves the game from the
// schedule cache instead of the live snapshot.
func (m *Menu) scheduleGameSubmenu(gameID string) []menuet.MenuItem {
	var g Game
	found := false
	m.mu.Lock()
	for _, entry := range m.scheduleCache {
		for _, sg := range entry.games {
			if sg.ID == gameID {
				g = sg
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	m.mu.Unlock()
	if !found {
		return []menuet.MenuItem{labelItem("Game no longer available", menuet.WeightRegular)}
	}
	now := time.Now()
	revealed := m.cfg.Revealed(g)
	items := []menuet.MenuItem{
		labelItem(g.Matchup(), menuet.WeightBold),
	}
	if revealed {
		items = append(items, scoreLines(g)...)
		items = append(items, labelItem(gameTimeLabel(g, now), menuet.WeightRegular))
		items = append(items, menuet.Separator{})
		items = append(items, menuet.Regular{
			Text: "Hide scores",
			Clicked: func() {
				m.cfg.Hide(g.ID)
				menuet.App().MenuChanged()
			},
		})
	} else if g.State == StateFinal {
		items = append(items, labelItem(g.SummaryHidden(now), menuet.WeightRegular))
		items = append(items, menuet.Separator{})
		items = append(items, menuet.Regular{
			Text: "Show scores",
			Clicked: func() {
				m.cfg.Reveal(g.ID)
				menuet.App().MenuChanged()
			},
		})
	} else {
		items = append(items, labelItem(g.SummaryHidden(now), menuet.WeightRegular))
	}
	items = appendOpenInESPN(items, g)
	return items
}

// teamSchedule returns the cached schedule for a team, kicking off a refresh
// in the background if the cache is stale or empty. The second return value
// is whether the data is fresh (true) or a stale / empty placeholder.
func (m *Menu) teamSchedule(league League, teamID string) ([]Game, bool) {
	key := league.Key + ":" + teamID
	m.mu.Lock()
	entry := m.scheduleCache[key]
	stale := time.Since(entry.fetched) > m.scheduleCacheTTL
	if !entry.loading && (len(entry.games) == 0 || stale) {
		entry.loading = true
		m.scheduleCache[key] = entry
		m.mu.Unlock()
		go m.fetchSchedule(league, teamID, key)
		// fall through: return current (possibly empty) snapshot
		m.mu.Lock()
		entry = m.scheduleCache[key]
	}
	games := entry.games
	fresh := !stale && len(games) > 0
	m.mu.Unlock()
	return games, fresh
}

func (m *Menu) fetchSchedule(league League, teamID, key string) {
	games, err := FetchTeamSchedule(league, teamID)
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.scheduleCache[key]
	entry.loading = false
	if err != nil {
		// keep previously cached games (if any) but reset fetched so we retry
		// on the next open
		m.scheduleCache[key] = entry
		return
	}
	entry.games = games
	entry.fetched = time.Now()
	m.scheduleCache[key] = entry
	menuet.App().MenuChanged()
}

func leagueLabel(key string) string {
	if l, ok := LeagueByKey(key); ok {
		return l.Label
	}
	return key
}

func (m *Menu) teamSubmenu(league League) []menuet.MenuItem {
	return []menuet.MenuItem{
		menuet.Search{
			Placeholder: fmt.Sprintf("Filter %s teams", league.Label),
			Results: func(query string) []menuet.MenuItem {
				return m.teamResults(league, query)
			},
		},
	}
}

func (m *Menu) teamResults(league League, query string) []menuet.MenuItem {
	teams := m.teams(league)
	if len(teams) == 0 {
		return []menuet.MenuItem{menuet.Regular{Text: "Loading teams…"}}
	}
	sort.Slice(teams, func(i, j int) bool { return teams[i].DisplayName < teams[j].DisplayName })
	q := strings.ToLower(strings.TrimSpace(query))
	items := make([]menuet.MenuItem, 0, len(teams))
	for _, t := range teams {
		t := t
		if q != "" && !strings.Contains(strings.ToLower(t.DisplayName), q) {
			continue
		}
		fav := m.cfg.IsFavorite(league.Key, t.ID)
		items = append(items, menuet.Regular{
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
		menuet.Regular{
			Text:  "Show scores by default",
			State: m.cfg.ScoresByDefault(),
			Clicked: func() {
				m.cfg.SetScoresByDefault(!m.cfg.ScoresByDefault())
				menuet.App().MenuChanged()
				m.refreshTitle()
			},
		},
		menuet.Separator{},
		menuet.Regular{
			Text:  "Notify when game starts",
			State: m.cfg.NotifyGameStart(),
			Clicked: func() {
				m.cfg.SetNotifyGameStart(!m.cfg.NotifyGameStart())
				menuet.App().MenuChanged()
			},
		},
		menuet.Regular{
			Text:  "Notify when game ends",
			State: m.cfg.NotifyGameEnd(),
			Clicked: func() {
				m.cfg.SetNotifyGameEnd(!m.cfg.NotifyGameEnd())
				menuet.App().MenuChanged()
			},
		},
		menuet.Regular{
			Text:  "Notify on lead change (revealed games only)",
			State: m.cfg.NotifyLeadChange(),
			Clicked: func() {
				m.cfg.SetNotifyLeadChange(!m.cfg.NotifyLeadChange())
				menuet.App().MenuChanged()
			},
		},
		menuet.Separator{},
		menuet.Regular{
			Text:     "Leagues",
			Children: m.leaguesSubmenu,
		},
	}
}

// leaguesSubmenu surfaces every available league grouped by category. Each
// group is its own sub-submenu so the top-level Leagues list stays short.
func (m *Menu) leaguesSubmenu() []menuet.MenuItem {
	grouped := LeaguesGrouped()
	items := make([]menuet.MenuItem, 0, len(LeagueGroupOrder))
	for _, group := range LeagueGroupOrder {
		leagues, ok := grouped[group]
		if !ok || len(leagues) == 0 {
			continue
		}
		ls := leagues
		enabledCount := 0
		for _, l := range ls {
			if m.cfg.IsLeagueEnabled(l.Key) {
				enabledCount++
			}
		}
		items = append(items, menuet.Regular{
			Text:     fmt.Sprintf("%s (%d/%d)", group, enabledCount, len(ls)),
			Children: func() []menuet.MenuItem { return m.leaguesInGroup(ls) },
		})
	}
	return items
}

func (m *Menu) leaguesInGroup(leagues []League) []menuet.MenuItem {
	return []menuet.MenuItem{
		menuet.Search{
			Placeholder: "Filter leagues",
			Results: func(query string) []menuet.MenuItem {
				return m.leagueGroupResults(leagues, query)
			},
		},
	}
}

func (m *Menu) leagueGroupResults(leagues []League, query string) []menuet.MenuItem {
	q := strings.ToLower(strings.TrimSpace(query))
	items := make([]menuet.MenuItem, 0, len(leagues))
	for _, l := range leagues {
		l := l
		if q != "" && !strings.Contains(strings.ToLower(l.Label), q) {
			continue
		}
		enabled := m.cfg.IsLeagueEnabled(l.Key)
		items = append(items, menuet.Regular{
			Text:  l.Label,
			State: enabled,
			Clicked: func() {
				m.cfg.SetLeagueEnabled(l.Key, !enabled)
				menuet.App().MenuChanged()
			},
		})
	}
	return items
}

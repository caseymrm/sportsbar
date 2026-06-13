package main

import (
	"fmt"
	"os"
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
	// Skip the local in-process logo server when the binary is running in
	// menuet's snapshot mode — those URLs only resolve inside this process,
	// so a JSON snapshot captured for menuet.app would point at a dead host
	// (http://127.0.0.1:<random>/...). With logos == nil, the logo helpers
	// fall back to ESPN's canonical URLs which are publicly fetchable.
	if os.Getenv("MENUET_SNAPSHOT_PATH") == "" {
		m.logos = NewLogoServer(m.teams)
	}
	// Eagerly prime team catalogs for leagues with favorites so logos render
	// on first menu open instead of waiting until the user opens the picker.
	for _, f := range cfg.Favorites() {
		if l, ok := LeagueByKey(f.League); ok {
			m.teams(l)
		}
	}
	return m
}

// teamLogoURL returns a stable URL for the team's logo. Prefers the local
// white-disc composite served by LogoServer when it's running (live menu
// rendering); falls back to ESPN's canonical CDN URL when the local server
// isn't available (snapshot mode, or before the catalog has loaded). Both
// kinds of URL can be fed straight to menuet.Regular.Image.
func (m *Menu) teamLogoURL(leagueKey, teamID string) string {
	if u := m.logos.URL(leagueKey, teamID); u != "" {
		return u
	}
	league, ok := LeagueByKey(leagueKey)
	if !ok {
		return ""
	}
	for _, t := range m.teams(league) {
		if t.ID == teamID {
			return t.Logo()
		}
	}
	return ""
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
// behind them. If the server isn't running (snapshot mode) or hasn't bound,
// falls back to ESPN's appearance-appropriate variant.
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

// Title builds the menubar status item per the redesign's "focus + count"
// rule: show only the single most-relevant game (FavoriteGames() is already
// pre-sorted by relevance) and collapse the rest into a dim " +N" tail. The
// leading status-item Image is that game's favorite-team logo.
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
	top := games[0]
	topFav, hasFav := favoriteInGame(top, favs)
	favTeamID := topFav.TeamID
	revealed := m.cfg.Revealed(top)

	runs := top.TitleRuns(favTeamID, revealed, now)
	if extra := len(games) - 1; extra > 0 {
		runs = append(runs, r(fmt.Sprintf("  +%d", extra), ter11))
	}

	state := &menuet.MenuState{Runs: runs}
	if hasFav {
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
			// Track whether we've already placed a LIVE badge so at most one
			// row in the dropdown carries it — per the design's "one pill max
			// keeps the badge meaningful" rule. FavoriteGames is pre-sorted
			// by relevance (live first), so the first live game wins.
			badgePlaced := false
			for _, g := range games {
				withBadge := !badgePlaced && g.State == StateLive
				if withBadge {
					badgePlaced = true
				}
				items = append(items, m.gameItem(g, now, withBadge))
			}
		}
		items = append(items, menuet.Separator{})
		for _, f := range favs {
			f := f
			// Per the redesign: drop the (LEAGUE) suffix on favorite rows —
			// if it's your favorite you already know its league. League tags
			// stay on the *game* rows where opponent context still earns them.
			items = append(items, menuet.Regular{
				Text:     f.Name,
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

// gameItem renders one row of the main dropdown per the redesign:
//   - 20px team logo (Image)
//   - rich-text matchup as Runs
//   - dim Subtitle with league + state / time / affordance
//
// withLiveBadge appends a trailing LIVE pill to live games — the caller
// passes true only for the single most-relevant live game in the menu so we
// don't end up with multiple badges (one pill max, per the design).
func (m *Menu) gameItem(g Game, now time.Time, withLiveBadge bool) menuet.MenuItem {
	revealed := m.cfg.Revealed(g)
	favTeamID := ""
	for _, f := range m.cfg.Favorites() {
		if g.InvolvesTeam(f.League, f.TeamID) {
			favTeamID = f.TeamID
			break
		}
	}

	runs := dropdownGameRuns(g, favTeamID, revealed)
	if withLiveBadge && g.State == StateLive {
		runs = append(runs, r("  ", runOpts{}), r("LIVE", runOpts{
			color: menuet.SystemRed,
			badge: true,
		}))
	}

	subtitle := dropdownGameSubtitle(g, revealed, now)

	gid := g.ID
	item := menuet.Regular{
		Runs:     runs,
		Subtitle: subtitle,
		Children: func() []menuet.MenuItem { return m.gameSubmenu(gid) },
	}
	if favTeamID != "" {
		// 20px logo. Live menu: local white-disc composite that survives the
		// translucent menu background. Snapshot mode: ESPN canonical URL so
		// menuet.app can fetch it.
		item.Image = m.teamLogoURL(g.LeagueKey, favTeamID)
	}
	return item
}

// dropdownGameRuns builds the primary (top-line) runs for a dropdown row.
// Matches the JSX twoLine.primary patterns in DropdownV2 / variants-v2.jsx:
//
//	live revealed   : OUR ·semibold S·bold·mono  – ·tertiary T·sec·mono  OPP·sec
//	live hidden     : OUR @ OPP ·secondary
//	final revealed  : OUR ·semibold S·bold·mono  – ·tertiary T·sec·mono  OPP·sec  (won)
//	                  OUR ·sec·mono S·sec·mono  – ·tertiary T·bold·mono  OPP·sec   (lost)
//	final hidden    : OUR @ OPP ·secondary
//	upcoming        : OUR @ OPP
func dropdownGameRuns(g Game, favTeamID string, revealed bool) []menuet.TextRun {
	ourAbbr, oppAbbr := favAndOpponentAbbr(g, favTeamID)

	switch g.State {
	case StateUpcoming:
		return []menuet.TextRun{r(ourAbbr+" @ "+oppAbbr, runOpts{})}

	case StateLive:
		if !revealed {
			return []menuet.TextRun{r(ourAbbr+" @ "+oppAbbr, sec)}
		}
		ourScore, theirScore := scoresFor(g, favTeamID)
		return goldDropdownRow(ourAbbr, ourScore, oppAbbr, theirScore, ourScore >= theirScore)

	case StateFinal:
		if !revealed {
			return []menuet.TextRun{r(ourAbbr+" @ "+oppAbbr, sec)}
		}
		ourScore, theirScore := scoresFor(g, favTeamID)
		return goldDropdownRow(ourAbbr, ourScore, oppAbbr, theirScore, ourScore >= theirScore)
	}
	return []menuet.TextRun{r(ourAbbr+" @ "+oppAbbr, runOpts{})}
}

// dropdownGameSubtitle builds the dim second-line text per state:
//
//	live           : NBA · Q3 5:42
//	final hidden   : MLB · Final · reveal score
//	final revealed : MLB · Final · was Fri
//	upcoming       : NFL · Sun 1:25 PM
//
// NSMenuItem.subtitle ignores per-run colors (the system applies its own
// "subtitle" styling), so we emit a single concatenated run with " · "
// joiners — the visible separators here are intentional because we're not
// styling the chunks distinctly.
func dropdownGameSubtitle(g Game, revealed bool, now time.Time) []menuet.TextRun {
	parts := []string{g.LeagueLabel}
	switch g.State {
	case StateLive:
		parts = append(parts, liveDetailLabel(g))
	case StateFinal:
		if revealed {
			parts = append(parts, "Final", pastWithVerb("", g.endTimeEstimate(), now))
		} else {
			parts = append(parts, "Final", "reveal score")
		}
	case StateUpcoming:
		parts = append(parts, upcomingWhenLabel(g.Start, now))
	}
	// Drop any empty fragments — pastWithVerb with an empty verb returns
	// "just now" / weekday / date and never empty, but defensive anyway.
	clean := parts[:0]
	for _, p := range parts {
		if p != "" {
			clean = append(clean, p)
		}
	}
	return []menuet.TextRun{{Text: strings.Join(clean, " · ")}}
}

// liveDetailLabel returns ShortDetail if ESPN supplied one (e.g. "Q3 5:42"),
// otherwise a derived Q{N} {clock}. For baseball the caret form (▲5 / ▼5)
// stands in for "Top 5th" / "Bot 5th" so the subtitle matches the menubar.
func liveDetailLabel(g Game) string {
	if isBaseball(g.LeagueKey) {
		if c := baseballInningCaret(g.ShortDetail); c != "" {
			return c
		}
	}
	if g.ShortDetail != "" {
		return g.ShortDetail
	}
	if g.Period > 0 && g.Clock != "" {
		return fmt.Sprintf("Q%d %s", g.Period, g.Clock)
	}
	return "live"
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
	return m.quietScoreboardSubmenu(g)
}

// quietScoreboardSubmenu builds the redesign's "quiet scoreboard" game-detail
// submenu (variants-submenu.jsx → GameSubmenu1):
//
//	OUR @ THEM   NBA          ← matchup·bold + league·tertiary·11
//	[logo] AWAY   68          ← mono, secondary if trailer
//	[logo] HOME   71          ← mono, bold if leader
//	● Q3 5:42                 ← only when live, both runs systemRed
//	─────────
//	Hide / Show scores
//	─────────
//	Open in ESPN
func (m *Menu) quietScoreboardSubmenu(g Game) []menuet.MenuItem {
	revealed := m.cfg.Revealed(g)
	items := []menuet.MenuItem{
		menuet.Regular{
			Runs: []menuet.TextRun{
				r(g.Matchup(), bold),
				r("  "+g.LeagueLabel, ter11),
			},
			Clicked: func() {}, // keep enabled-looking (not greyed)
		},
	}
	if revealed && g.State != StateUpcoming {
		items = append(items, m.teamScoreRow(g, g.Away, g.AwayScore, g.AwayScore > g.HomeScore))
		items = append(items, m.teamScoreRow(g, g.Home, g.HomeScore, g.HomeScore > g.AwayScore))
		if g.State == StateLive {
			items = append(items, liveClockRow(g))
		}
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
		items = append(items, menuet.Regular{
			Runs:    []menuet.TextRun{r(g.SummaryHidden(time.Now()), sec)},
			Clicked: func() {},
		})
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
		items = append(items, menuet.Regular{
			Runs:    []menuet.TextRun{r(g.SummaryHidden(time.Now()), sec)},
			Clicked: func() {},
		})
	}
	items = appendOpenInESPN(items, g)
	return items
}

// teamScoreRow renders one team's line in the quiet scoreboard. Leader gets
// a gold underline under the team abbreviation and a gold-tinted score
// (no underline) — the same split-style rule used by the menubar title,
// dropdown, and team-schedule winners.
func (m *Menu) teamScoreRow(g Game, team EspnTeam, score int, leader bool) menuet.MenuItem {
	abbrStyle := mono
	scoreStyle := mono
	if leader {
		abbrStyle = goldWinnerAbbrStyle(menuet.WeightBold, true)
		scoreStyle = goldWinnerScoreStyle(menuet.WeightBold, true)
	}
	row := menuet.Regular{
		Runs: []menuet.TextRun{
			r(team.Abbreviation, abbrStyle),
			r("   ", runOpts{mono: true}),
			r(fmt.Sprintf("%d", score), scoreStyle),
		},
		Clicked: func() {},
	}
	row.Image = m.teamLogoURL(g.LeagueKey, team.ID)
	return row
}

// liveClockRow is the per-quarter clock line under the team scores. Both runs
// take SystemRed so the row reads as the "live" beat — the only red in the
// otherwise quiet scoreboard.
func liveClockRow(g Game) menuet.MenuItem {
	clock := g.ShortDetail
	if clock == "" && g.Period > 0 {
		clock = fmt.Sprintf("Q%d %s", g.Period, g.Clock)
	}
	if clock == "" {
		clock = "live"
	}
	return menuet.Regular{
		Runs: []menuet.TextRun{
			r("● ", runOpts{color: menuet.SystemRed, size: 10}),
			r(clock, runOpts{color: menuet.SystemRed, mono: true}),
		},
		Clicked: func() {},
	}
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

// sectionHeader renders a small-caps section label per the redesign — 11px
// tertiary, e.g. "RECENT" / "UPCOMING" / "SCORES" / "NOTIFICATIONS". Stays
// greyed (no Clicked) because it isn't interactive; the secondary-tone color
// is what makes it read as a header rather than disabled noise.
func sectionHeader(text string) menuet.MenuItem {
	return menuet.Regular{
		Runs: []menuet.TextRun{r(text, ter11)},
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

	now := time.Now()
	recent := []Game{}
	upcoming := []Game{}

	// Live games for this favorite always belong in Recent — even when
	// they're also shown at the top of the menu. The schedule endpoint
	// may or may not surface them (status depends on ESPN's per-league
	// formatting), so we draw them from the live snapshot first and
	// dedupe schedule entries against this list.
	included := map[string]bool{}
	for _, g := range m.p.FavoriteGames() {
		if g.State == StateLive && g.InvolvesTeam(f.League, f.TeamID) {
			recent = append(recent, g)
			included[g.ID] = true
		}
	}

	// Exclude *non-live* today's games (finished today, upcoming today)
	// from the schedule loop so they don't duplicate the top-of-menu rows.
	visible := map[string]bool{}
	for _, g := range m.p.FavoriteGames() {
		if !included[g.ID] {
			visible[g.ID] = true
		}
	}

	for _, g := range sched {
		if visible[g.ID] || included[g.ID] {
			continue
		}
		switch g.State {
		case StateFinal, StateLive:
			// Live games count as Recent — they're in the past relative to
			// "starts at" and the row's own ● marker tells you they're not
			// finished yet. "Upcoming" is reserved for games that haven't
			// kicked off.
			recent = append(recent, g)
		case StateUpcoming:
			upcoming = append(upcoming, g)
		}
	}
	// Recent: most-recent first. Live games count as "now" for sort purposes
	// — they're the closest thing to being a past event without actually
	// being one yet, so they outrank any final's start time. Finals sort
	// among themselves by their own start time descending.
	sortNow := time.Now()
	sort.Slice(recent, func(i, j int) bool {
		ti := recent[i].Start
		if recent[i].State == StateLive {
			ti = sortNow
		}
		tj := recent[j].Start
		if recent[j].State == StateLive {
			tj = sortNow
		}
		return ti.After(tj)
	})
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
		items = append(items, sectionHeader("RECENT"))
		for _, g := range recent {
			items = append(items, m.scheduleGameItem(g, now, f.TeamID))
		}
	}
	if len(upcoming) > 0 {
		items = append(items, sectionHeader("UPCOMING"))
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
// scheduleGameItem renders one row in a team's Recent or Upcoming list.
// Ports the schedRow / upRow builders from design_handoff/variants-v2.jsx.
// Finals use a fixed-column scoreboard layout (W/L/? heavy result + two
// mono-padded score blocks + dim day column); upcoming rows use a left-
// indented "OUR @ OPP" form with a day/time tail.
func (m *Menu) scheduleGameItem(g Game, now time.Time, favTeamID string) menuet.MenuItem {
	revealed := m.cfg.Revealed(g)
	var runs []menuet.TextRun
	switch g.State {
	case StateFinal:
		runs = schedFinalRow(g, favTeamID, revealed)
	case StateUpcoming:
		runs = schedUpcomingRow(g, favTeamID, now)
	default:
		runs = schedFinalRow(g, favTeamID, revealed) // live in a schedule list is rare
	}
	gid := g.ID
	return menuet.Regular{
		Runs:     runs,
		Children: func() []menuet.MenuItem { return m.scheduleGameSubmenu(gid) },
	}
}

// goldDropdownRow is the per-row format for the main dropdown's revealed
// game lines. Winner side (team + score) takes the gold tint + WeightBold +
// the trophy halo; loser side stays at default LabelPrimary with
// WeightRegular. Center dash is neutral.
func goldDropdownRow(ourAbbr string, ourScore int, oppAbbr string, theirScore int, weWin bool) []menuet.TextRun {
	var ourAbbrS, ourScoreS, theirScoreS, oppAbbrS runOpts
	if weWin {
		ourAbbrS = goldWinnerAbbrStyle(menuet.WeightSemibold, false)
		ourScoreS = goldWinnerScoreStyle(menuet.WeightBold, true)
		theirScoreS = mono
		oppAbbrS = plain
	} else {
		ourAbbrS = runOpts{weight: menuet.WeightSemibold}
		ourScoreS = mono
		theirScoreS = goldWinnerScoreStyle(menuet.WeightBold, true)
		oppAbbrS = goldWinnerAbbrStyle(menuet.WeightRegular, false)
	}
	// Underline only sits under letters and digits — the gap spaces and the
	// center " – " stay untreated.
	return []menuet.TextRun{
		r(ourAbbr, ourAbbrS),
		r(" ", runOpts{}),
		r(fmt.Sprintf("%d", ourScore), ourScoreS),
		r(" – ", ter),
		r(fmt.Sprintf("%d", theirScore), theirScoreS),
		r(" ", runOpts{}),
		r(oppAbbr, oppAbbrS),
	}
}

// schedFinalRow ports schedRow() from variants-v2.jsx — the canonical mono
// scoreboard with W/L/? result column and right-aligned 3-char score fields.
//
//	result(2)  [TEAM(3) score→3]  gap(2)  [OPP(3) score→3]  gap(3)  day
//
// Padding the team abbreviation right and the score left keeps the columns
// stable across 1-, 2-, and 3-digit scores AND 2- vs 3-letter abbrs — the
// thing that makes basketball (98 / 112 / 121) and baseball (3 / 5 / 11)
// stack the same way without tab stops.
// liveOrFinalResultMarker chooses the 2-char marker that sits in the result
// column on a non-hidden row: a SystemRed "● " for in-progress games, or
// two blank mono spaces for completed finals. Stays the same width either
// way so the rest of the row's columns hold.
func liveOrFinalResultMarker(g Game) menuet.TextRun {
	if g.State == StateLive {
		return r("● ", runOpts{
			mono:   true,
			color:  menuet.SystemRed,
			weight: menuet.WeightBold,
		})
	}
	return r("  ", runOpts{mono: true})
}

func schedFinalRow(g Game, favTeamID string, revealed bool) []menuet.TextRun {
	ourAbbr, oppAbbr := favAndOpponentAbbr(g, favTeamID)
	ourScore, oppScore := scoresFor(g, favTeamID)
	hidden := !revealed
	won := ourScore > oppScore

	// Result column carries: "? " on hidden rows (spoiler veil), "● " on
	// live rows (in-progress marker, SystemRed), or two mono spaces on
	// final rows (the W/L letter is gone — winner side gets the gold
	// treatment instead). Live games still get the gold treatment on the
	// current leader, since the score really is the leader's right now.
	var result menuet.TextRun
	var ourStyle, ourNumStyle, oppStyle, oppNumStyle runOpts
	switch {
	case hidden:
		result = r("? ", runOpts{mono: true, color: menuet.LabelQuaternary, weight: menuet.WeightHeavy})
		ourStyle, ourNumStyle = monoSec, monoSec
		oppStyle, oppNumStyle = monoSec, monoSec
	case won:
		result = liveOrFinalResultMarker(g)
		ourStyle = goldWinnerAbbrStyle(menuet.WeightBold, true)
		ourNumStyle = goldWinnerScoreStyle(menuet.WeightBold, true)
		oppStyle, oppNumStyle = mono, mono
	default:
		result = liveOrFinalResultMarker(g)
		ourStyle, ourNumStyle = mono, mono
		oppStyle = goldWinnerAbbrStyle(menuet.WeightBold, true)
		oppNumStyle = goldWinnerScoreStyle(menuet.WeightBold, true)
	}

	ourScoreText := fmt.Sprintf("%d", ourScore)
	oppScoreText := fmt.Sprintf("%d", oppScore)
	if hidden {
		ourScoreText = "?"
		oppScoreText = "?"
		// Per JSX: when hidden the score-slot run carries the quaternary
		// veil color on top of the team's secondary tone, so the `?` reads
		// quieter than the abbreviation beside it.
		veiledStyle := runOpts{mono: true, color: menuet.LabelQuaternary}
		ourNumStyle, oppNumStyle = veiledStyle, veiledStyle
	}

	// Split each padded slot into "pad" + "glyphs" so the underline (when
	// present) stays under the actual characters only — never under the
	// alignment whitespace before a 1-digit score or after a 2-letter abbr.
	day := scheduleDayLabel(g.Start)
	monoGap := runOpts{mono: true}
	runs := []menuet.TextRun{result}
	runs = append(runs, padRRuns(ourAbbr, 3, withMono(ourStyle))...)
	runs = append(runs, r(" ", monoGap))
	runs = append(runs, padLRuns(ourScoreText, 3, withMono(ourNumStyle))...)
	runs = append(runs, r("  ", monoGap))
	runs = append(runs, padRRuns(oppAbbr, 3, withMono(oppStyle))...)
	runs = append(runs, r(" ", monoGap))
	runs = append(runs, padLRuns(oppScoreText, 3, withMono(oppNumStyle))...)
	runs = append(runs, r("   "+day, monoTerTiny))
	return runs
}

// padLRuns is the right-aligned (left-padded) counterpart for scores. The
// leading pad spaces become their own mono-only run so any underline /
// background applied to the glyph style doesn't draw under the alignment
// whitespace.
func padLRuns(text string, width int, style runOpts) []menuet.TextRun {
	pad := width - len(text)
	if pad <= 0 {
		return []menuet.TextRun{r(text, style)}
	}
	return []menuet.TextRun{
		r(spaces(pad), runOpts{mono: true}),
		r(text, style),
	}
}

// padRRuns splits a left-aligned (right-padded) abbreviation slot into
// "glyphs" + "trailing pad" runs so emphasis on the abbr's style doesn't
// bleed onto the alignment whitespace.
func padRRuns(text string, width int, style runOpts) []menuet.TextRun {
	pad := width - len(text)
	if pad <= 0 {
		return []menuet.TextRun{r(text, style)}
	}
	return []menuet.TextRun{
		r(text, style),
		r(spaces(pad), runOpts{mono: true}),
	}
}

// schedUpcomingRow ports upRow() — no score, two-space left indent to share
// the result column's margin, day + time tail in tertiary.
func schedUpcomingRow(g Game, favTeamID string, now time.Time) []menuet.TextRun {
	ourAbbr, oppAbbr := favAndOpponentAbbr(g, favTeamID)
	when := upcomingWhenLabel(g.Start, now)
	return []menuet.TextRun{
		r("  ", runOpts{mono: true}),
		r(ourAbbr+" @ "+oppAbbr, runOpts{mono: true}),
		r("   "+when, monoTerTiny),
	}
}

// scheduleDayLabel returns the day-of-week for the row's far-right column,
// in three-letter uppercase per the design ("FRI", "MON").
func scheduleDayLabel(t time.Time) string {
	return strings.ToUpper(t.Local().Format("Mon"))
}

// upcomingWhenLabel returns "Thu 7:30pm"-style text for upcoming rows.
// Uses 12-hour time, lowercase am/pm to match the design.
func upcomingWhenLabel(start, now time.Time) string {
	t := start.Local()
	weekday := t.Format("Mon")
	clock := t.Format("3:04pm")
	return weekday + " " + clock
}

// withMono ensures Monospaced is set on a runOpts even if the caller passed
// a token (sec/bold/heavy) that didn't include it. Avoids accidentally
// dropping mono when reusing the secondary color alias.
func withMono(o runOpts) runOpts {
	o.mono = true
	return o
}

// scheduleGameSubmenu mirrors gameSubmenu but resolves the game from the
// schedule cache instead of the live snapshot. Reuses the same quiet
// scoreboard layout for visual consistency between today's games and
// schedule entries.
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
	return m.quietScoreboardSubmenu(g)
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

// settingsSubmenu ports SettingsMenu from variants-submenu.jsx — small-caps
// section headers replace separators, row labels become terse two-word
// phrases, and parenthetical context becomes a dim 11px tertiary run appended
// to the row.
func (m *Menu) settingsSubmenu() []menuet.MenuItem {
	enabledTotal := 0
	for _, l := range Leagues {
		if m.cfg.IsLeagueEnabled(l.Key) {
			enabledTotal++
		}
	}
	return []menuet.MenuItem{
		sectionHeader("SCORES"),
		menuet.Regular{
			Text:  "Show scores by default",
			State: m.cfg.ScoresByDefault(),
			Clicked: func() {
				m.cfg.SetScoresByDefault(!m.cfg.ScoresByDefault())
				menuet.App().MenuChanged()
				m.refreshTitle()
			},
		},
		sectionHeader("NOTIFICATIONS"),
		menuet.Regular{
			Text:  "Game starts",
			State: m.cfg.NotifyGameStart(),
			Clicked: func() {
				m.cfg.SetNotifyGameStart(!m.cfg.NotifyGameStart())
				menuet.App().MenuChanged()
			},
		},
		menuet.Regular{
			Text:  "Game ends",
			State: m.cfg.NotifyGameEnd(),
			Clicked: func() {
				m.cfg.SetNotifyGameEnd(!m.cfg.NotifyGameEnd())
				menuet.App().MenuChanged()
			},
		},
		menuet.Regular{
			Runs: []menuet.TextRun{
				r("Lead changes", runOpts{}),
				r("  revealed games only", ter11),
			},
			State: m.cfg.NotifyLeadChange(),
			Clicked: func() {
				m.cfg.SetNotifyLeadChange(!m.cfg.NotifyLeadChange())
				menuet.App().MenuChanged()
			},
		},
		menuet.Separator{},
		menuet.Regular{
			Runs: []menuet.TextRun{
				r("Leagues", runOpts{}),
				r(fmt.Sprintf("  %d of %d enabled", enabledTotal, len(Leagues)), ter11),
			},
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

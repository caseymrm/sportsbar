package main

import (
	"sync"
	"time"

	"github.com/caseymrm/menuet/v2"
)

const (
	keyFavorites        = "sportsbar.favorites"
	keyRevealed         = "sportsbar.revealed"
	keyTeamPrefs        = "sportsbar.teamPrefs"
	keyEnabledLeagues   = "sportsbar.enabledLeagues"
	keyDefaultShow      = "sportsbar.scoresByDefault"
	keyNotifyGameStart  = "sportsbar.notifyGameStart"
	keyNotifyGameEnd    = "sportsbar.notifyGameEnd"
	keyNotifyLeadChange = "sportsbar.notifyLeadChange"
	keyInitialized      = "sportsbar.initialized"
)

// DefaultEnabledLeagues is the seed set installed on first launch — the four
// big US sports that sportsbar originally shipped with. Existing users keep
// whatever they've toggled; only fresh installs hit this.
var DefaultEnabledLeagues = []string{"nfl", "nba", "mlb", "nhl"}

const MaxFavorites = 4

type Favorite struct {
	League string `json:"league"`
	TeamID string `json:"teamId"`
	Abbr   string `json:"abbr"`
	Name   string `json:"name"`
}

// TeamPrefs holds per-team overrides for each global setting. nil means "use
// the global value"; *true / *false are explicit overrides.
type TeamPrefs struct {
	ScoresByDefault  *bool `json:"scoresByDefault,omitempty"`
	NotifyGameStart  *bool `json:"notifyGameStart,omitempty"`
	NotifyGameEnd    *bool `json:"notifyGameEnd,omitempty"`
	NotifyLeadChange *bool `json:"notifyLeadChange,omitempty"`
}

// PrefField names a single per-team override so menu code can address them
// without hardcoding four parallel methods.
type PrefField int

const (
	PrefScoresByDefault PrefField = iota
	PrefNotifyGameStart
	PrefNotifyGameEnd
	PrefNotifyLeadChange
)

func (p *TeamPrefs) field(f PrefField) **bool {
	switch f {
	case PrefScoresByDefault:
		return &p.ScoresByDefault
	case PrefNotifyGameStart:
		return &p.NotifyGameStart
	case PrefNotifyGameEnd:
		return &p.NotifyGameEnd
	case PrefNotifyLeadChange:
		return &p.NotifyLeadChange
	}
	return nil
}

func teamKey(league, teamID string) string { return league + ":" + teamID }

type Config struct {
	mu sync.RWMutex

	favorites      []Favorite
	revealed       map[string]int64
	teamPrefs      map[string]TeamPrefs
	enabledLeagues map[string]bool

	scoresByDefault  bool
	notifyGameStart  bool
	notifyGameEnd    bool
	notifyLeadChange bool
}

func LoadConfig() *Config {
	d := menuet.Defaults()
	// First-run defaults: notifications on. menuet's Defaults.Boolean can't
	// distinguish "unset" from "false", so we gate first-run with an init
	// marker. enabledLeagues isn't seeded here — it's handled by the
	// post-load fallback below so existing installs upgrading to this
	// version (which already passed first-run init) also get seeded.
	if !d.Boolean(keyInitialized) {
		d.SetBoolean(keyNotifyGameStart, true)
		d.SetBoolean(keyNotifyGameEnd, true)
		d.SetBoolean(keyNotifyLeadChange, true)
		d.SetBoolean(keyInitialized, true)
	}
	c := &Config{
		revealed:         make(map[string]int64),
		teamPrefs:        make(map[string]TeamPrefs),
		enabledLeagues:   make(map[string]bool),
		scoresByDefault:  d.Boolean(keyDefaultShow),
		notifyGameStart:  d.Boolean(keyNotifyGameStart),
		notifyGameEnd:    d.Boolean(keyNotifyGameEnd),
		notifyLeadChange: d.Boolean(keyNotifyLeadChange),
	}
	_ = d.Unmarshal(keyFavorites, &c.favorites)
	_ = d.Unmarshal(keyRevealed, &c.revealed)
	_ = d.Unmarshal(keyTeamPrefs, &c.teamPrefs)
	_ = d.Unmarshal(keyEnabledLeagues, &c.enabledLeagues)
	if c.revealed == nil {
		c.revealed = make(map[string]int64)
	}
	if c.teamPrefs == nil {
		c.teamPrefs = make(map[string]TeamPrefs)
	}
	if c.enabledLeagues == nil {
		c.enabledLeagues = make(map[string]bool)
	}
	// Seed default leagues if none are enabled — covers both fresh installs
	// and upgrades from versions that predate this setting.
	if len(c.enabledLeagues) == 0 {
		for _, k := range DefaultEnabledLeagues {
			c.enabledLeagues[k] = true
		}
		_ = d.Marshal(keyEnabledLeagues, c.enabledLeagues)
	}
	return c
}

// IsLeagueEnabled reports whether the user has the given league turned on.
func (c *Config) IsLeagueEnabled(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.enabledLeagues[key]
}

// SetLeagueEnabled toggles a league on or off, persisting the change.
func (c *Config) SetLeagueEnabled(key string, v bool) {
	c.mu.Lock()
	if v {
		c.enabledLeagues[key] = true
	} else {
		delete(c.enabledLeagues, key)
	}
	c.mu.Unlock()
	_ = menuet.Defaults().Marshal(keyEnabledLeagues, c.enabledLeagues)
}

// EnabledLeagueKeys returns the set of currently-enabled league keys (no
// ordering guarantee — caller sorts if needed).
func (c *Config) EnabledLeagueKeys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.enabledLeagues))
	for k, v := range c.enabledLeagues {
		if v {
			out = append(out, k)
		}
	}
	return out
}

func (c *Config) Favorites() []Favorite {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Favorite, len(c.favorites))
	copy(out, c.favorites)
	return out
}

// ToggleFavorite adds or removes a team. Returns true if the favorite is
// present after the call, false if removed or capped out.
func (c *Config) ToggleFavorite(f Favorite) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, existing := range c.favorites {
		if existing.League == f.League && existing.TeamID == f.TeamID {
			c.favorites = append(c.favorites[:i], c.favorites[i+1:]...)
			c.persistFavoritesLocked()
			return false
		}
	}
	if len(c.favorites) >= MaxFavorites {
		return false
	}
	c.favorites = append(c.favorites, f)
	c.persistFavoritesLocked()
	return true
}

func (c *Config) IsFavorite(leagueKey, teamID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, f := range c.favorites {
		if f.League == leagueKey && f.TeamID == teamID {
			return true
		}
	}
	return false
}

// Revealed reports whether the game's score should be shown. Precedence:
//   1. explicit per-game reveal flag set by the user
//   2. per-team "show scores by default" override on any followed team in the game
//   3. global "show scores by default"
//
// Multi-team-favorite edge case (you follow both teams in the game): if any
// followed team says "show", we show. Opt-in actions fire if any opt-in is true.
func (c *Config) Revealed(g Game) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if _, ok := c.revealed[g.ID]; ok {
		return true
	}
	for _, f := range c.favorites {
		if !g.InvolvesTeam(f.League, f.TeamID) {
			continue
		}
		if p, ok := c.teamPrefs[teamKey(f.League, f.TeamID)]; ok && p.ScoresByDefault != nil {
			if *p.ScoresByDefault {
				return true
			}
		}
	}
	return c.scoresByDefault
}

// RevealedExplicit reports whether the user has explicitly opted in for this
// specific game (the per-game flag), ignoring team or global defaults.
func (c *Config) RevealedExplicit(gameID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.revealed[gameID]
	return ok
}

// TeamPref returns the override for a single field on a team, or nil meaning
// "use global default".
func (c *Config) TeamPref(league, teamID string, field PrefField) *bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	prefs, ok := c.teamPrefs[teamKey(league, teamID)]
	if !ok {
		return nil
	}
	p := prefs.field(field)
	if p == nil || *p == nil {
		return nil
	}
	v := **p
	return &v
}

// SetTeamPref sets a per-team override. Pass nil to revert to the global default.
func (c *Config) SetTeamPref(league, teamID string, field PrefField, v *bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := teamKey(league, teamID)
	prefs := c.teamPrefs[key]
	p := prefs.field(field)
	if p == nil {
		return
	}
	if v == nil {
		*p = nil
	} else {
		val := *v
		*p = &val
	}
	if prefs.isEmpty() {
		delete(c.teamPrefs, key)
	} else {
		c.teamPrefs[key] = prefs
	}
	_ = menuet.Defaults().Marshal(keyTeamPrefs, c.teamPrefs)
}

func (p TeamPrefs) isEmpty() bool {
	return p.ScoresByDefault == nil && p.NotifyGameStart == nil &&
		p.NotifyGameEnd == nil && p.NotifyLeadChange == nil
}

// EffectiveNotify reports whether to fire the given notification kind for a
// game involving the user's favorites. Most-aggressive: if any followed team
// in the game says yes (or falls back to a global yes), notify.
func (c *Config) EffectiveNotify(g Game, field PrefField) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	globalOn := false
	switch field {
	case PrefNotifyGameStart:
		globalOn = c.notifyGameStart
	case PrefNotifyGameEnd:
		globalOn = c.notifyGameEnd
	case PrefNotifyLeadChange:
		globalOn = c.notifyLeadChange
	default:
		return false
	}
	any := false
	allOverride := true
	for _, f := range c.favorites {
		if !g.InvolvesTeam(f.League, f.TeamID) {
			continue
		}
		any = true
		prefs, ok := c.teamPrefs[teamKey(f.League, f.TeamID)]
		if !ok {
			allOverride = false
			continue
		}
		p := prefs.field(field)
		if p == nil || *p == nil {
			allOverride = false
			continue
		}
		if **p {
			return true
		}
	}
	if !any {
		return globalOn
	}
	if allOverride {
		// every followed team in this game explicitly said "off"
		return false
	}
	return globalOn
}

func (c *Config) Reveal(gameID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.revealed[gameID]; ok {
		return
	}
	c.revealed[gameID] = time.Now().Unix()
	c.persistRevealedLocked()
}

func (c *Config) Hide(gameID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.revealed[gameID]; !ok {
		return
	}
	delete(c.revealed, gameID)
	c.persistRevealedLocked()
}

// PruneRevealed drops reveal entries older than age. Called periodically so
// the dict doesn't grow forever.
func (c *Config) PruneRevealed(age time.Duration) {
	cutoff := time.Now().Add(-age).Unix()
	c.mu.Lock()
	defer c.mu.Unlock()
	changed := false
	for id, ts := range c.revealed {
		if ts < cutoff {
			delete(c.revealed, id)
			changed = true
		}
	}
	if changed {
		c.persistRevealedLocked()
	}
}

func (c *Config) ScoresByDefault() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.scoresByDefault
}

func (c *Config) SetScoresByDefault(v bool) {
	c.mu.Lock()
	c.scoresByDefault = v
	c.mu.Unlock()
	menuet.Defaults().SetBoolean(keyDefaultShow, v)
}

func (c *Config) NotifyGameStart() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.notifyGameStart
}

func (c *Config) SetNotifyGameStart(v bool) {
	c.mu.Lock()
	c.notifyGameStart = v
	c.mu.Unlock()
	menuet.Defaults().SetBoolean(keyNotifyGameStart, v)
}

func (c *Config) NotifyGameEnd() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.notifyGameEnd
}

func (c *Config) SetNotifyGameEnd(v bool) {
	c.mu.Lock()
	c.notifyGameEnd = v
	c.mu.Unlock()
	menuet.Defaults().SetBoolean(keyNotifyGameEnd, v)
}

func (c *Config) NotifyLeadChange() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.notifyLeadChange
}

func (c *Config) SetNotifyLeadChange(v bool) {
	c.mu.Lock()
	c.notifyLeadChange = v
	c.mu.Unlock()
	menuet.Defaults().SetBoolean(keyNotifyLeadChange, v)
}

func (c *Config) persistFavoritesLocked() {
	_ = menuet.Defaults().Marshal(keyFavorites, c.favorites)
}

func (c *Config) persistRevealedLocked() {
	_ = menuet.Defaults().Marshal(keyRevealed, c.revealed)
}

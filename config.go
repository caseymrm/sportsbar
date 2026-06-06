package main

import (
	"sync"
	"time"

	"github.com/caseymrm/menuet"
)

const (
	keyFavorites        = "sportsbar.favorites"
	keyRevealed         = "sportsbar.revealed"
	keyDefaultShow      = "sportsbar.scoresByDefault"
	keyNotifyGameStart  = "sportsbar.notifyGameStart"
	keyNotifyGameEnd    = "sportsbar.notifyGameEnd"
	keyNotifyLeadChange = "sportsbar.notifyLeadChange"
	keyInitialized      = "sportsbar.initialized"
)

const MaxFavorites = 4

type Favorite struct {
	League string `json:"league"`
	TeamID string `json:"teamId"`
	Abbr   string `json:"abbr"`
	Name   string `json:"name"`
}

type Config struct {
	mu sync.RWMutex

	favorites []Favorite
	revealed  map[string]int64

	scoresByDefault  bool
	notifyGameStart  bool
	notifyGameEnd    bool
	notifyLeadChange bool
}

func LoadConfig() *Config {
	d := menuet.Defaults()
	// First-run defaults: notifications on. menuet.Defaults.Boolean cannot
	// distinguish "unset" from "false", so we gate this with an init marker.
	if !d.Boolean(keyInitialized) {
		d.SetBoolean(keyNotifyGameStart, true)
		d.SetBoolean(keyNotifyGameEnd, true)
		d.SetBoolean(keyNotifyLeadChange, true)
		d.SetBoolean(keyInitialized, true)
	}
	c := &Config{
		revealed:         make(map[string]int64),
		scoresByDefault:  d.Boolean(keyDefaultShow),
		notifyGameStart:  d.Boolean(keyNotifyGameStart),
		notifyGameEnd:    d.Boolean(keyNotifyGameEnd),
		notifyLeadChange: d.Boolean(keyNotifyLeadChange),
	}
	_ = d.Unmarshal(keyFavorites, &c.favorites)
	_ = d.Unmarshal(keyRevealed, &c.revealed)
	if c.revealed == nil {
		c.revealed = make(map[string]int64)
	}
	return c
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

func (c *Config) Revealed(gameID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.scoresByDefault {
		return true
	}
	_, ok := c.revealed[gameID]
	return ok
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

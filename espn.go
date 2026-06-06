package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type League struct {
	Key   string
	Sport string
	Label string
}

var Leagues = []League{
	{Key: "nfl", Sport: "football", Label: "NFL"},
	{Key: "nba", Sport: "basketball", Label: "NBA"},
	{Key: "mlb", Sport: "baseball", Label: "MLB"},
	{Key: "nhl", Sport: "hockey", Label: "NHL"},
}

func LeagueByKey(key string) (League, bool) {
	for _, l := range Leagues {
		if l.Key == key {
			return l, true
		}
	}
	return League{}, false
}

type EspnTeam struct {
	ID           string `json:"id"`
	Abbreviation string `json:"abbreviation"`
	DisplayName  string `json:"displayName"`
	Location     string `json:"location"`
	Name         string `json:"name"`
	LogoURL      string `json:"logo,omitempty"`
}

type scoreboardResponse struct {
	Events []scoreboardEvent `json:"events"`
}

type scoreboardEvent struct {
	ID           string                  `json:"id"`
	Date         espnTime                `json:"date"`
	Name         string                  `json:"name"`
	ShortName    string                  `json:"shortName"`
	Status       scoreboardStatus        `json:"status"`
	Competitions []scoreboardCompetition `json:"competitions"`
}

// espnTime parses ESPN's scoreboard timestamps, which are minute-precision
// UTC ("2026-06-06T18:30Z") rather than full RFC 3339 — Go's default
// time.Time UnmarshalJSON rejects the missing seconds.
type espnTime struct{ time.Time }

func (t *espnTime) UnmarshalJSON(b []byte) error {
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		b = b[1 : len(b)-1]
	}
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	for _, layout := range []string{
		"2006-01-02T15:04Z",
		time.RFC3339,
		time.RFC3339Nano,
	} {
		if parsed, err := time.Parse(layout, string(b)); err == nil {
			t.Time = parsed
			return nil
		}
	}
	return fmt.Errorf("unrecognized espn timestamp: %q", string(b))
}

type scoreboardStatus struct {
	DisplayClock string `json:"displayClock"`
	Period       int    `json:"period"`
	Type         struct {
		State       string `json:"state"`
		Completed   bool   `json:"completed"`
		Description string `json:"description"`
		Detail      string `json:"detail"`
		ShortDetail string `json:"shortDetail"`
	} `json:"type"`
}

type scoreboardCompetition struct {
	Competitors []scoreboardCompetitor `json:"competitors"`
}

type scoreboardCompetitor struct {
	HomeAway string   `json:"homeAway"`
	Score    string   `json:"score"`
	Team     EspnTeam `json:"team"`
}

var espnHTTP = &http.Client{Timeout: 10 * time.Second}

func FetchScoreboard(league League, date time.Time) ([]Game, error) {
	u := fmt.Sprintf("https://site.api.espn.com/apis/site/v2/sports/%s/%s/scoreboard",
		league.Sport, league.Key)
	if !date.IsZero() {
		u += "?dates=" + date.Format("20060102")
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "sportsbar/0.1 (+https://github.com/caseymrm/sportsbar)")
	resp, err := espnHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("espn scoreboard %s: %s", league.Key, resp.Status)
	}
	var sb scoreboardResponse
	if err := json.NewDecoder(resp.Body).Decode(&sb); err != nil {
		return nil, fmt.Errorf("decode espn scoreboard %s: %w", league.Key, err)
	}
	games := make([]Game, 0, len(sb.Events))
	for _, ev := range sb.Events {
		if g, ok := gameFromEvent(league, ev); ok {
			games = append(games, g)
		}
	}
	return games, nil
}

func gameFromEvent(league League, ev scoreboardEvent) (Game, bool) {
	if len(ev.Competitions) == 0 || len(ev.Competitions[0].Competitors) < 2 {
		return Game{}, false
	}
	var home, away scoreboardCompetitor
	for _, c := range ev.Competitions[0].Competitors {
		if c.HomeAway == "home" {
			home = c
		} else {
			away = c
		}
	}
	if home.Team.ID == "" || away.Team.ID == "" {
		return Game{}, false
	}
	g := Game{
		ID:          ev.ID,
		LeagueKey:   league.Key,
		LeagueLabel: league.Label,
		Start:       ev.Date.Time,
		Home:        home.Team,
		Away:        away.Team,
		HomeScore:   parseScore(home.Score),
		AwayScore:   parseScore(away.Score),
		Clock:       ev.Status.DisplayClock,
		Period:      ev.Status.Period,
		ShortDetail: ev.Status.Type.ShortDetail,
	}
	switch ev.Status.Type.State {
	case "pre":
		g.State = StateUpcoming
	case "in":
		g.State = StateLive
	case "post":
		g.State = StateFinal
	default:
		g.State = StateUpcoming
	}
	return g, true
}

func parseScore(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

type teamsResponse struct {
	Sports []struct {
		Leagues []struct {
			Teams []struct {
				Team EspnTeam `json:"team"`
			} `json:"teams"`
		} `json:"leagues"`
	} `json:"sports"`
}

func FetchTeams(league League) ([]EspnTeam, error) {
	u := fmt.Sprintf("https://site.api.espn.com/apis/site/v2/sports/%s/%s/teams",
		league.Sport, league.Key)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "sportsbar/0.1 (+https://github.com/caseymrm/sportsbar)")
	resp, err := espnHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("espn teams %s: %s", league.Key, resp.Status)
	}
	var tr teamsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("decode espn teams %s: %w", league.Key, err)
	}
	if len(tr.Sports) == 0 || len(tr.Sports[0].Leagues) == 0 {
		return nil, errors.New("espn teams: empty response")
	}
	out := make([]EspnTeam, 0, 32)
	for _, t := range tr.Sports[0].Leagues[0].Teams {
		out = append(out, t.Team)
	}
	return out, nil
}

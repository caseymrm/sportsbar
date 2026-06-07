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
	Links        []scoreboardLink        `json:"links"`
}

type scoreboardLink struct {
	Rel  []string `json:"rel"`
	Href string   `json:"href"`
	Text string   `json:"text"`
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
	HomeAway string    `json:"homeAway"`
	Score    espnScore `json:"score"`
	Winner   bool      `json:"winner"`
	Team     EspnTeam  `json:"team"`
}

// espnScore handles two score formats: the scoreboard endpoint sends a plain
// string ("107"), while the team-schedule endpoint sends an object
// ({"value":107.0,"displayValue":"107"}). One type, one decoder, both paths.
type espnScore int

func (s *espnScore) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*s = 0
		return nil
	}
	var str string
	if err := json.Unmarshal(b, &str); err == nil {
		if str == "" {
			*s = 0
			return nil
		}
		if n, err := strconv.Atoi(str); err == nil {
			*s = espnScore(n)
			return nil
		}
		*s = 0
		return nil
	}
	var obj struct {
		Value        float64 `json:"value"`
		DisplayValue string  `json:"displayValue"`
	}
	if err := json.Unmarshal(b, &obj); err == nil {
		if obj.DisplayValue != "" {
			if n, err := strconv.Atoi(obj.DisplayValue); err == nil {
				*s = espnScore(n)
				return nil
			}
		}
		*s = espnScore(int(obj.Value))
		return nil
	}
	*s = 0
	return nil
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
		HomeScore:   int(home.Score),
		AwayScore:   int(away.Score),
		Clock:       ev.Status.DisplayClock,
		Period:      ev.Status.Period,
		ShortDetail: ev.Status.Type.ShortDetail,
		Links:       linksByRel(ev.Links),
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

type teamsResponse struct {
	Sports []struct {
		Leagues []struct {
			Teams []struct {
				Team EspnTeam `json:"team"`
			} `json:"teams"`
		} `json:"leagues"`
	} `json:"sports"`
}

// scheduleResponse is the team-schedule shape — the same event/competition
// structure as the scoreboard, but no live status state field (we derive
// state from the winner flag and the date instead).
type scheduleResponse struct {
	Events []scoreboardEvent `json:"events"`
}

// FetchTeamSchedule returns the team's known games for the current season,
// ordered by date. State is derived from the winner field plus date, since
// the schedule endpoint omits status.type.state.
func FetchTeamSchedule(league League, teamID string) ([]Game, error) {
	u := fmt.Sprintf("https://site.api.espn.com/apis/site/v2/sports/%s/%s/teams/%s/schedule",
		league.Sport, league.Key, teamID)
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
		return nil, fmt.Errorf("espn schedule %s/%s: %s", league.Key, teamID, resp.Status)
	}
	var sr scheduleResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("decode espn schedule %s/%s: %w", league.Key, teamID, err)
	}
	now := time.Now()
	games := make([]Game, 0, len(sr.Events))
	for _, ev := range sr.Events {
		g, ok := gameFromEvent(league, ev)
		if !ok {
			continue
		}
		// Schedule events have no live state — derive from winner flag and date.
		// (gameFromEvent set state from the absent status field, defaulting to
		// Upcoming. Override here with a more accurate inference.)
		if anyWinner(ev) {
			g.State = StateFinal
		} else if g.Start.Before(now) {
			// In the past but no winner declared — probably postponed or in
			// progress. Treat as Final so the user at least sees it surface.
			g.State = StateFinal
		} else {
			g.State = StateUpcoming
		}
		games = append(games, g)
	}
	return games, nil
}

// linksByRel keys ESPN's link list by the first element of each link's rel
// array, which is the category ("summary", "boxscore", "recap", etc.).
func linksByRel(ls []scoreboardLink) map[string]string {
	if len(ls) == 0 {
		return nil
	}
	out := make(map[string]string, len(ls))
	for _, l := range ls {
		if l.Href == "" || len(l.Rel) == 0 {
			continue
		}
		if _, exists := out[l.Rel[0]]; exists {
			continue
		}
		out[l.Rel[0]] = l.Href
	}
	return out
}

func anyWinner(ev scoreboardEvent) bool {
	if len(ev.Competitions) == 0 {
		return false
	}
	for _, c := range ev.Competitions[0].Competitors {
		if c.Winner {
			return true
		}
	}
	return false
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

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
	Group string // "US Pro" / "College" / "Soccer" — for grouping in settings UI
}

// Leagues is every team-sport league we verified returns a scoreboard with our
// expected two-competitor shape. Probed against ESPN's site API and limited to
// those that returned 200 and a usable event structure (golf/tennis/racing/MMA
// excluded — different domain model; rugby/cricket/netball excluded — ESPN
// serves those via separate feeds not on site.api.espn.com).
var Leagues = []League{
	{Key: "nfl", Sport: "football", Label: "NFL", Group: "US Pro"},
	{Key: "nba", Sport: "basketball", Label: "NBA", Group: "US Pro"},
	{Key: "wnba", Sport: "basketball", Label: "WNBA", Group: "US Pro"},
	{Key: "mlb", Sport: "baseball", Label: "MLB", Group: "US Pro"},
	{Key: "nhl", Sport: "hockey", Label: "NHL", Group: "US Pro"},

	{Key: "college-football", Sport: "football", Label: "NCAA Football", Group: "College"},
	{Key: "mens-college-basketball", Sport: "basketball", Label: "NCAA Men's Basketball", Group: "College"},
	{Key: "womens-college-basketball", Sport: "basketball", Label: "NCAA Women's Basketball", Group: "College"},
	{Key: "college-baseball", Sport: "baseball", Label: "NCAA Baseball", Group: "College"},
	{Key: "mens-college-hockey", Sport: "hockey", Label: "NCAA Men's Hockey", Group: "College"},
	{Key: "mens-college-lacrosse", Sport: "lacrosse", Label: "NCAA Men's Lacrosse", Group: "College"},

	// International domestic leagues (sorted roughly by global stature/visibility)
	{Key: "eng.1", Sport: "soccer", Label: "Premier League", Group: "International"},
	{Key: "esp.1", Sport: "soccer", Label: "La Liga", Group: "International"},
	{Key: "ita.1", Sport: "soccer", Label: "Serie A", Group: "International"},
	{Key: "ger.1", Sport: "soccer", Label: "Bundesliga", Group: "International"},
	{Key: "fra.1", Sport: "soccer", Label: "Ligue 1", Group: "International"},
	{Key: "ned.1", Sport: "soccer", Label: "Eredivisie", Group: "International"},
	{Key: "por.1", Sport: "soccer", Label: "Primeira Liga", Group: "International"},
	{Key: "usa.1", Sport: "soccer", Label: "MLS", Group: "International"},
	{Key: "mex.1", Sport: "soccer", Label: "Liga MX", Group: "International"},
	{Key: "usa.nwsl", Sport: "soccer", Label: "NWSL", Group: "International"},
	{Key: "bra.1", Sport: "soccer", Label: "Brazilian Série A", Group: "International"},
	{Key: "arg.1", Sport: "soccer", Label: "Argentine Liga Profesional", Group: "International"},
	{Key: "afl", Sport: "australian-football", Label: "AFL", Group: "International"},

	// Domestic cups
	{Key: "eng.fa", Sport: "soccer", Label: "FA Cup", Group: "International"},
	{Key: "eng.league_cup", Sport: "soccer", Label: "Carabao Cup", Group: "International"},
	{Key: "esp.copa_del_rey", Sport: "soccer", Label: "Copa del Rey", Group: "International"},

	// Continental club competitions
	{Key: "uefa.champions", Sport: "soccer", Label: "UEFA Champions League", Group: "International"},
	{Key: "uefa.europa", Sport: "soccer", Label: "UEFA Europa League", Group: "International"},
	{Key: "concacaf.champions", Sport: "soccer", Label: "Concacaf Champions Cup", Group: "International"},
	{Key: "conmebol.libertadores", Sport: "soccer", Label: "Copa Libertadores", Group: "International"},
	{Key: "afc.champions", Sport: "soccer", Label: "AFC Champions League Elite", Group: "International"},

	// National-team tournaments
	{Key: "fifa.world", Sport: "soccer", Label: "FIFA World Cup", Group: "International"},
	{Key: "uefa.euro", Sport: "soccer", Label: "UEFA European Championship", Group: "International"},
	{Key: "uefa.nations", Sport: "soccer", Label: "UEFA Nations League", Group: "International"},
	{Key: "concacaf.gold", Sport: "soccer", Label: "Concacaf Gold Cup", Group: "International"},
	{Key: "conmebol.america", Sport: "soccer", Label: "Copa América", Group: "International"},
	{Key: "fifa.confederations", Sport: "soccer", Label: "FIFA Confederations Cup", Group: "International"},
}

// LeagueGroupOrder is the display ordering for the settings UI. New groups
// added to a League definition should be appended here too.
var LeagueGroupOrder = []string{"US Pro", "College", "International"}

// LeaguesGrouped returns Leagues partitioned by Group, preserving the order
// inside each group (which is the declaration order in Leagues).
func LeaguesGrouped() map[string][]League {
	out := make(map[string][]League)
	for _, l := range Leagues {
		out[l.Group] = append(out[l.Group], l)
	}
	return out
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
	ID           string    `json:"id"`
	Abbreviation string    `json:"abbreviation"`
	DisplayName  string    `json:"displayName"`
	Location     string    `json:"location"`
	Name         string    `json:"name"`
	LogoURL      string    `json:"logo,omitempty"`  // scoreboard endpoint: flat string
	Logos        []logoRef `json:"logos,omitempty"` // teams + schedule endpoints: variants array
}

type logoRef struct {
	Href string   `json:"href"`
	Rel  []string `json:"rel"`
}

// Logo returns the best available logo URL for this team. Prefers the flat
// LogoURL (set by scoreboard responses), then a Logos entry with rel=default,
// then the first Logos entry. Returns "" if the team has no logo data.
func (t EspnTeam) Logo() string {
	if t.LogoURL != "" {
		return t.LogoURL
	}
	if u := t.logoByRel("default"); u != "" {
		return u
	}
	if len(t.Logos) > 0 {
		return t.Logos[0].Href
	}
	return ""
}

// LogoDark returns ESPN's dark-mode logo variant if one is published (rel=dark,
// typically the logo in white with no background), falling back to the default.
// ESPN's primary_logo_on_white_color was tempting but is published as a huge
// 4096-square canvas with the logo small inside — resizing to 16px makes the
// logo content tiny and unreadable.
func (t EspnTeam) LogoDark() string {
	if u := t.logoByRel("dark"); u != "" {
		return u
	}
	return t.Logo()
}

func (t EspnTeam) logoByRel(target string) string {
	for _, l := range t.Logos {
		for _, r := range l.Rel {
			if r == target {
				return l.Href
			}
		}
	}
	return ""
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

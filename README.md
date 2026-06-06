# sportsbar

A macOS menubar app for NFL, NBA, MLB, and NHL scores, built around the unusual goal of *not* spoiling games you haven't watched yet.

## What it does

- Follow up to 4 favorite teams across the four big US leagues.
- The menubar title shows *whether* your team is currently playing or recently played, **without** showing the score by default.
- Reveal scores per game by clicking. The reveal sticks until that game ages out, so you don't have to keep re-confirming.
- Optional notifications for game start (at actual first pitch / opening tip / kickoff, not 5 minutes before), game end, and lead changes. Lead-change notifications only fire for games you've explicitly revealed, since they inherently spoil.
- Smart polling: tighter cadence when a game is live or about to start, idle when nothing's happening. Idle leagues are not polled at all.

## Status

Early. The spoiler-aware UX is the point; rough edges everywhere else are expected.

## Building

```sh
go build .
```

Notifications require running inside a real `.app` bundle (a limitation of `NSStatusItem`-backed apps on modern macOS). The plain binary works for the menubar UI on its own.

## Data source

Scores come from ESPN's unofficial public scoreboard JSON — the same feed ESPN's own widgets use. It's not a documented API and can change without notice; if a fetch starts failing, that's the most likely cause.

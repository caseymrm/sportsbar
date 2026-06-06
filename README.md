# sportsbar

A macOS menubar app for NFL, NBA, MLB, and NHL scores, built around the unusual goal of *not* spoiling games you haven't watched yet.

## What it does

- Follow up to 4 favorite teams across the four big US leagues.
- The menubar shows *whether* your team is currently playing or recently played, **without** showing the score by default.
- Reveal scores per game by clicking. The reveal sticks until that game ages out, so you don't have to keep re-confirming.
- Optional notifications for game start (at actual first pitch / opening tip / kickoff, not 5 minutes before), game end, and lead changes. Lead-change notifications only fire for games you've explicitly revealed, since they inherently spoil.
- Smart polling: tighter cadence when a game is live or about to start, idle when nothing's happening. Idle leagues are not polled at all.

## Install

No prebuilt binaries yet — you'll need Go and Xcode command-line tools.

```sh
git clone https://github.com/caseymrm/sportsbar
cd sportsbar
make            # builds Sportsbar.app/ in the project directory
cp -R Sportsbar.app /Applications/
open /Applications/Sportsbar.app
```

The `make` step uses [menuet](https://github.com/caseymrm/menuet)'s shared bundling recipe — it produces a real `.app` bundle (with `Info.plist`) so notifications work on modern macOS. Building without bundling (`go build .`) gives you a bare binary that runs in the menubar but can't surface notifications.

## Status

Early. The spoiler-aware UX is the point; rough edges everywhere else are expected.

## Data source

Scores come from ESPN's unofficial public scoreboard JSON — the same feed ESPN's own widgets use. It's not a documented API and can change without notice; if a fetch starts failing, that's the most likely cause.

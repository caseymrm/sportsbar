# sportsbar

A macOS menubar app for sports scores across 32 leagues (US Pro, College, International), built around the unusual goal of *not* spoiling games you haven't watched yet.

## What it does

- Follow up to 4 favorite teams.
- The menubar shows *whether* your team is currently playing or recently played, **without** showing the score by default.
- Reveal scores per game by clicking. The reveal sticks until that game ages out, so you don't have to keep re-confirming.
- Optional notifications for game start (at actual first pitch / opening tip / kickoff, not 5 minutes before), game end, and lead changes. Lead-change notifications only fire for games you've explicitly revealed, since they inherently spoil.
- Smart polling: tighter cadence when a game is live or about to start, idle when nothing's happening. Idle leagues are not polled at all.
- Auto-updates: when a new release ships, you'll be prompted to install it the next time the app phones home (once a day).

## Install

**[Download the latest release](https://github.com/caseymrm/sportsbar/releases/latest/download/Sportsbar.zip)**, unzip, drag `Sportsbar.app` to `/Applications`, and open it.

Releases are signed and notarized (since v0.2.0), so it opens without any Gatekeeper warning.

### Build from source

If you'd rather build it yourself (or are on an unsupported macOS):

```sh
git clone https://github.com/caseymrm/sportsbar
cd sportsbar
make            # builds Sportsbar.app/ as a universal arm64+amd64 binary
cp -R Sportsbar.app /Applications/
open /Applications/Sportsbar.app
```

You'll need Go (>= 1.22) and the Xcode command-line tools (`xcode-select --install`). The Makefile uses [menuet](https://github.com/caseymrm/menuet)'s shared bundling recipe to produce a real `.app` bundle with `Info.plist`, which modern macOS requires for notifications to surface.

## Status

Early. The spoiler-aware UX is the point; rough edges everywhere else are expected.

## Data source

Scores come from ESPN's unofficial public scoreboard JSON — the same feed ESPN's own widgets use. It's not a documented API and can change without notice; if a fetch starts failing, that's the most likely cause.

package main

import (
	"fmt"

	"github.com/caseymrm/menuet/v2"
)

// runs.go ports the JSX `r(text, opts)` helper and design tokens from the
// sportsbar redesign handoff. The redesign rule is: every fact gets its own
// voice (weight + color + size + monospaced) and we delete the literal " · "
// and " | " separators that used to do this work.
//
// Token reference (from the design handoff):
//   identity                 = LabelPrimary, WeightSemibold
//   secondary                = LabelSecondary (opponent, opponent score, verbs)
//   tertiary (11px)          = LabelTertiary  (league tag, day column, +N count)
//   quaternary               = LabelQuaternary (`?` spoiler veil)
//   live / loss / destructive= SystemRed
//   win                      = SystemGreen
//   leading / winning score  = WeightBold + Monospaced
//   W / L / ? letter         = WeightHeavy
//   badge                    = TextRun{Badge: true, Color: SystemRed}

// runOpts mirrors the JSX r() options object. Zero values inherit the row's
// defaults — same semantics as menuet's TextRun. Mirrors the menuet v2.8
// surface, so any new emphasis (underline, strikethrough, background,
// shadow) can be applied through the same r() builder.
type runOpts struct {
	color         menuet.Color
	weight        menuet.FontWeight
	size          int
	mono          bool
	badge         bool
	underline     bool
	strikethrough bool
	background    menuet.Color
	shadow        *menuet.Shadow
}

// r builds a TextRun. Mirrors the JSX `r(text, opts)` builder so porting the
// design files reads close to the original.
func r(text string, o runOpts) menuet.TextRun {
	return menuet.TextRun{
		Text:          text,
		Color:         o.color,
		FontSize:      o.size,
		FontWeight:    o.weight,
		Monospaced:    o.mono,
		Badge:         o.badge,
		Underline:     o.underline,
		Strikethrough: o.strikethrough,
		Background:    o.background,
		Shadow:        o.shadow,
	}
}

// Convenience aliases for the runs you write most often. These read closer to
// the JSX than spelling out runOpts every time.
var (
	semibold = runOpts{weight: menuet.WeightSemibold}
	bold     = runOpts{weight: menuet.WeightBold}
	heavy    = runOpts{weight: menuet.WeightHeavy}

	sec     = runOpts{color: menuet.LabelSecondary}
	ter     = runOpts{color: menuet.LabelTertiary}
	quat    = runOpts{color: menuet.LabelQuaternary}
	red     = runOpts{color: menuet.SystemRed}
	green   = runOpts{color: menuet.SystemGreen}

	monoBold     = runOpts{weight: menuet.WeightBold, mono: true}
	monoSec      = runOpts{color: menuet.LabelSecondary, mono: true}
	monoTerTiny  = runOpts{color: menuet.LabelTertiary, mono: true, size: 10}
	ter11        = runOpts{color: menuet.LabelTertiary, size: 11}

	// Trailing team in a finished or live game. The original design called
	// for LabelSecondary on the loser, but at small sizes that grey reads as
	// "disabled" and is hard to scan. We keep the loser at LabelPrimary
	// (default) and lean on the WeightBold vs WeightRegular split — plus the
	// loud W/L marker — to carry the leader/trailer distinction. AppKit
	// doesn't expose a semantic between primary and secondary, so a fixed
	// RGBA would have broken dark-mode adaptation.
	mono         = runOpts{mono: true}
	plain        = runOpts{}
)

// titleGold is the menubar title's "winner medal" tint. Loser side gets no
// treatment (default LabelPrimary, Regular weight) — just plain text — so
// the only thing standing out is the winner's gold + Bold. Avoids the
// "Christmas" two-color asymmetry of red+green and the alarm voice of
// SystemGreen/SystemRed.
//
// Fixed RGBA: AppKit doesn't have a semantic gold, and SystemYellow is too
// vivid. This midweight gold reads against both the light and the dark
// menubar's translucent background without re-tuning per appearance.
var titleGold = menuet.Color{R: 200, G: 165, B: 70, A: 255}

// winnerHalo is the celebration glow that wraps the winner's runs (menuet
// v2.8+). Blur with zero offsets renders as an isotropic glow around the
// text, separately from the gold foreground tint. The halo color is a
// brighter gold than the foreground so the glow reads as light spilling
// off the letters rather than a smudged copy.
//
// Dialed down from the initial Blur=6 / A=200: that was readable in the
// dropdown but smudged the letters in the small menubar title. Blur=3 and
// alpha 110 leaves a hint of warmth around the glyphs without bleeding
// into them.
var winnerHalo = &menuet.Shadow{
	Color: menuet.Color{R: 255, G: 220, B: 100, A: 110},
	Blur:  3,
}

// goldWinnerStyle is the canonical runOpts for any "this side won" run —
// gold tint, the requested weight, monospaced for digits, and the trophy
// halo. Caller picks the weight (Semibold for the identity abbr, Bold for
// the score).
func goldWinnerStyle(weight menuet.FontWeight, mono bool) runOpts {
	return runOpts{
		color:  titleGold,
		weight: weight,
		mono:   mono,
		shadow: winnerHalo,
	}
}

// padL right-aligns s in a field of n characters (left-padded with spaces).
// Used for score columns so 1-, 2-, and 3-digit scores align on their ones digit.
func padL(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return spaces(n-len(s)) + s
}

// padR left-aligns s in a field of n characters (right-padded with spaces).
// Used for team abbreviations so columns hold across 2- and 3-letter abbrs.
func padR(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + spaces(n-len(s))
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%*s", n, "")
}

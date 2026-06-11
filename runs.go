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
// defaults — same semantics as menuet's TextRun.
type runOpts struct {
	color    menuet.Color
	weight   menuet.FontWeight
	size     int
	mono     bool
	badge    bool
}

// r builds a TextRun. Mirrors the JSX `r(text, opts)` builder so porting the
// design files reads close to the original.
func r(text string, o runOpts) menuet.TextRun {
	return menuet.TextRun{
		Text:       text,
		Color:      o.color,
		FontSize:   o.size,
		FontWeight: o.weight,
		Monospaced: o.mono,
		Badge:      o.badge,
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

package main

import (
	"time"

	"github.com/caseymrm/menuet/v2"
)

// Version must match a GitHub release tag name exactly — menuet's auto-update
// does a string compare against tag_name. menuet's ecosystem convention is
// "v"-prefixed (whyawake "v0.9", notafan "v1.0.0"); matching that.
const Version = "v0.2.0"

func main() {
	cfg := LoadConfig()
	poller := NewPoller(cfg)
	menu := NewMenu(cfg, poller)

	// In demo-fixture mode we install curated Lakers + Dodgers state and
	// skip the live poller / notifier so the ESPN refresh loop can't
	// overwrite the fixture before the snapshot is taken.
	if fixtureMode() {
		installFixture(cfg, poller, menu)
	}

	wg, ctx := menuet.App().GracefulShutdownHandles()

	if !fixtureMode() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			poller.Run(ctx)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			RunNotifier(ctx, cfg, poller.Updates())
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		refresh := time.NewTicker(5 * time.Second)
		defer refresh.Stop()
		prune := time.NewTicker(15 * time.Minute)
		defer prune.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-refresh.C:
				menuet.App().SetMenuState(menu.Title())
			case <-prune.C:
				// Games age out of ESPN's slate after roughly a day; let the
				// reveal map follow the same TTL so it doesn't grow forever.
				cfg.PruneRevealed(48 * time.Hour)
			}
		}
	}()

	app := menuet.App()
	app.Name = "sportsbar"
	app.Label = "com.github.caseymrm.sportsbar"
	app.Children = menu.Children
	app.AutoUpdate.Version = Version
	app.AutoUpdate.Repo = "caseymrm/sportsbar"
	app.SetMenuState(menu.Title())
	app.RunApplication()
}

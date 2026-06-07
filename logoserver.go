package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
)

// LogoServer composites team logos onto a white circle and serves them over
// a localhost HTTP listener. menuet's image loader only accepts http:// URLs
// or bundle resource names — not file paths — so an in-process server is the
// cheapest way to feed it locally-rendered PNGs without patching menuet.
//
// The composite-on-circle approach makes team logos legible against
// translucent system menus (Dodgers blue on dark grey was unreadable; logo
// on white disc cuts through any background).
type LogoServer struct {
	teamsFor func(League) []EspnTeam

	mu      sync.Mutex
	cache   map[string][]byte // key: "league:teamId" -> PNG bytes
	baseURL string
}

// NewLogoServer binds a listener on a random localhost port and starts
// serving in the background. Returns nil baseURL on bind failure (caller
// then falls back to original logo URLs).
func NewLogoServer(teamsFor func(League) []EspnTeam) *LogoServer {
	ls := &LogoServer{
		teamsFor: teamsFor,
		cache:    make(map[string][]byte),
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Printf("logo server listen: %v", err)
		return ls
	}
	ls.baseURL = fmt.Sprintf("http://%s", ln.Addr().String())
	go func() {
		if err := http.Serve(ln, http.HandlerFunc(ls.handle)); err != nil {
			log.Printf("logo server: %v", err)
		}
	}()
	return ls
}

// URL returns the localhost URL for a (league, team) composite, or "" if the
// server failed to bind.
func (ls *LogoServer) URL(leagueKey, teamID string) string {
	if ls == nil || ls.baseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/%s.png", ls.baseURL, leagueKey, teamID)
}

func (ls *LogoServer) handle(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(strings.Trim(r.URL.Path, "/"), ".png")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	leagueKey, teamID := parts[0], parts[1]
	key := leagueKey + ":" + teamID

	ls.mu.Lock()
	cached, ok := ls.cache[key]
	ls.mu.Unlock()
	if ok {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(cached)
		return
	}

	league, ok := LeagueByKey(leagueKey)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var srcURL string
	for _, t := range ls.teamsFor(league) {
		if t.ID == teamID {
			srcURL = t.Logo()
			break
		}
	}
	if srcURL == "" {
		http.NotFound(w, r)
		return
	}

	resp, err := http.Get(srcURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	srcBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	out, err := compositeOnWhiteCircle(srcBytes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ls.mu.Lock()
	ls.cache[key] = out
	ls.mu.Unlock()
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(out)
}

// compositeOnWhiteCircle decodes a logo PNG, draws a white filled disc the
// size of the source plus 10% padding, then composites the logo centered on
// top. Caller-provided alpha is preserved via draw.Over. Output is encoded as
// PNG bytes. menuet will resize the result to its target height (16 for menu
// items, 22 for the status item) — we leave it at source resolution so
// downscaling has plenty of pixels to work with.
func compositeOnWhiteCircle(srcBytes []byte) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(srcBytes))
	if err != nil {
		return nil, fmt.Errorf("decode logo: %w", err)
	}
	srcBounds := src.Bounds()
	srcW, srcH := srcBounds.Dx(), srcBounds.Dy()
	pad := max2(srcW, srcH) / 10
	canvas := max2(srcW, srcH) + 2*pad
	out := image.NewRGBA(image.Rect(0, 0, canvas, canvas))
	center := canvas / 2
	radiusSq := center * center
	white := color.RGBA{255, 255, 255, 255}
	for y := 0; y < canvas; y++ {
		dy := y - center
		for x := 0; x < canvas; x++ {
			dx := x - center
			if dx*dx+dy*dy <= radiusSq {
				out.SetRGBA(x, y, white)
			}
		}
	}
	offX := (canvas - srcW) / 2
	offY := (canvas - srcH) / 2
	draw.Draw(out, image.Rect(offX, offY, offX+srcW, offY+srcH),
		src, srcBounds.Min, draw.Over)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, fmt.Errorf("encode composite: %w", err)
	}
	return buf.Bytes(), nil
}

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}

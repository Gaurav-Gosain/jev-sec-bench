// Command jev-tui shows the benchmark results as a terminal dashboard.
//
// It reads the committed JSON in results/ and renders it. With TYPESAFE_API_KEY
// set it can also stream live requests to Jev, one sample per row, which is the
// mode worth recording.
//
//	jev-tui                  read ./results and open the dashboard
//	jev-tui -results path    read results from somewhere else
//	jev-tui -tab live        open straight on the live feed
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	jev "github.com/Gaurav-Gosain/jev-go"
	"github.com/Gaurav-Gosain/jev-sec-bench/internal/ansisvg"
	"github.com/Gaurav-Gosain/jev-sec-bench/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "jev-tui: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("results", "results", "directory holding the benchmark result JSON")
	start := flag.String("tab", "overview", "tab to open on: overview, injection, code, live")
	shot := flag.String("shot", "", "render one frame at WxH and print it, instead of opening the dashboard")
	samples := flag.Int("samples", 12, "live samples to ask for before rendering a live screenshot")
	svgOut := flag.String("svg", "", "with -shot, also write the frame to this SVG file")
	flag.Parse()

	results, err := tui.Load(*dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no results in %s. Run: go run ./cmd/jev-sec-bench", *dir)
		}
		return err
	}

	// A key is optional. Without one the dashboard still renders everything that
	// was measured; it just cannot stream new samples.
	client, err := jev.New()
	if err != nil && !errors.Is(err, jev.ErrNoAPIKey) {
		return err
	}

	model := tui.New(results, client)
	tab, ok := tui.ParseTab(*start)
	if !ok {
		return fmt.Errorf("unknown tab %q: pick overview, injection, code or live", *start)
	}
	model = model.WithTab(tab)

	if *shot != "" {
		var w, h int
		if _, err := fmt.Sscanf(*shot, "%dx%d", &w, &h); err != nil {
			return fmt.Errorf("bad -shot size %q: want something like 120x40", *shot)
		}
		// The live tab is empty until something has been asked, so fill it with
		// real answers before drawing.
		if tab == tui.TabLive && *samples > 0 {
			filled, err := model.Prefill(context.Background(), *samples)
			if err != nil {
				return err
			}
			model = filled
		}
		frame := model.Render(w, h, tab)
		if *svgOut == "" {
			fmt.Println(frame)
			return nil
		}
		if err := os.WriteFile(*svgOut, []byte(ansisvg.Render(frame, ansisvg.Default())), 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", *svgOut, err)
		}
		fmt.Println("wrote", *svgOut)
		return nil
	}

	_, err = tea.NewProgram(model).Run()
	return err
}

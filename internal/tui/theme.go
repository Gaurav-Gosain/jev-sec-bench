// Package tui renders the benchmark results as a terminal dashboard.
package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"
)

// The palette. Grounds run dark to light, ink runs the other way, and the four
// signal colours are the only ones allowed to carry meaning.
var (
	canvas  = charmtone.Pepper // the page
	panel   = charmtone.BBQ    // a card on the page
	surface = charmtone.Char   // a cell inside a card
	rule    = charmtone.Iron   // borders and separators

	muted = charmtone.Squid // labels that should recede
	dim   = charmtone.Smoke // secondary text
	ink   = charmtone.Salt  // primary text

	accent  = charmtone.Charple // brand, chrome, selected state
	accent2 = charmtone.Hazy    // the lighter end of the brand ramp

	good = charmtone.Julep   // passing, correct, safe
	warn = charmtone.Mustard // borderline, review
	bad  = charmtone.Cherry  // failing, flagged, vulnerable
	info = charmtone.Malibu  // neutral highlight
)

// verdict maps a probability onto the colour that should carry it. The bands
// match the routing policy the README recommends: block, review, pass.
func verdict(p float64) color.Color {
	switch {
	case p >= 0.70:
		return bad
	case p >= 0.35:
		return warn
	default:
		return good
	}
}

// scale maps a 0 to 1 quality score onto a colour, where higher is better.
// This is the opposite direction from verdict, which is about hazard.
func scale(v float64) color.Color {
	switch {
	case v >= 0.90:
		return good
	case v >= 0.75:
		return charmtone.Guac
	case v >= 0.60:
		return warn
	default:
		return bad
	}
}

var (
	base = lipgloss.NewStyle().Foreground(ink)

	// text roles
	title    = lipgloss.NewStyle().Foreground(ink).Bold(true)
	label    = lipgloss.NewStyle().Foreground(muted)
	subtle   = lipgloss.NewStyle().Foreground(dim)
	strong   = lipgloss.NewStyle().Foreground(ink).Bold(true)
	accented = lipgloss.NewStyle().Foreground(accent2).Bold(true)

	// the brand mark in the header
	brand = lipgloss.NewStyle().
		Foreground(charmtone.Salt).
		Background(accent).
		Bold(true).
		Padding(0, 1)

	modelChip = lipgloss.NewStyle().
			Foreground(charmtone.Pepper).
			Background(good).
			Bold(true).
			Padding(0, 1)

	// tabs
	tabOn = lipgloss.NewStyle().
		Foreground(charmtone.Salt).
		Background(charmtone.Char).
		Bold(true).
		Padding(0, 2)
	tabOff = lipgloss.NewStyle().
		Foreground(muted).
		Padding(0, 2)

	cardTitle = lipgloss.NewStyle().
			Foreground(accent2).
			Bold(true)

	edge = lipgloss.NewStyle().Foreground(rule)

	footer = lipgloss.NewStyle().Foreground(muted)

	keyCap = lipgloss.NewStyle().
		Foreground(charmtone.Pepper).
		Background(charmtone.Oyster).
		Bold(true).
		Padding(0, 1)
)

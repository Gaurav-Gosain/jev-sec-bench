#!/usr/bin/env bash
# Regenerate the dashboard screenshots.
#
# The TUI renders one frame to SVG itself, so the pictures come from the same
# code that produced the numbers and cannot drift away from them. PNGs are for
# places that will not take an SVG.
#
# Needs rsvg-convert for the PNG step (brew install librsvg). The live tab makes
# real API calls, so TYPESAFE_API_KEY has to be set for it to fill in.
set -euo pipefail

cd "$(dirname "$0")/.."
go build -o bin/jev-tui ./cmd/jev-tui

shoot() {
  local tab=$1 size=$2
  shift 2
  ./bin/jev-tui -shot "$size" -tab "$tab" -svg "docs/$tab.svg" "$@" >/dev/null
  if command -v rsvg-convert >/dev/null; then
    rsvg-convert -o "docs/$tab.png" "docs/$tab.svg"
  fi
  echo "docs/$tab.svg"
}

shoot overview  118x44
shoot injection 118x46
shoot code      118x46

if [ -n "${TYPESAFE_API_KEY:-}" ]; then
  shoot live 118x40 -samples 16
else
  echo "skipping the live tab: TYPESAFE_API_KEY is not set"
fi

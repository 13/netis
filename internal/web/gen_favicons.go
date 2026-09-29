//go:build ignore

// gen_favicons writes the netis app mark and every icon a browser, a phone
// home screen or an installed app asks for, all from the one drawing below.
// Run it from internal/web after changing the mark:
//
//	go run gen_favicons.go
//
// It needs rsvg-convert (librsvg) and ImageMagick 7 (magick) on the PATH; they
// are only used here, never at runtime, and the output is committed.
//
// The mark is a network socket with its link light on: a white RJ45 jack on a
// patch-cable blue tile, with a link-green LED above it (the patch-panel
// vocabulary of the design brief). The drawing sits on a 32-unit grid with
// edges on even units, so each edge lands on a whole pixel at 16px.
package main

import (
	"fmt"
	"os"
	"os/exec"
)

const (
	accent     = "#1F5FD1" // --accent, light theme
	accentDark = "#3A74E6" // a lifted accent that holds up on a dark tab strip
	recess     = "#123A85" // the jack's opening, a shade of the accent
	link       = "#3CCB6A" // --link, dark theme: the brighter green reads on blue
)

// glyph is the jack and its LED, without the tile behind them.
const glyph = `<path fill="#fff" d="M4 10h24v14h-6v4H10v-4H4z"/>` +
	`<path fill="` + recess + `" d="M8 14h16v6h-4v4h-8v-4H8z"/>` +
	`<circle cx="24" cy="5" r="3" fill="` + link + `"/>`

func svg(body string) string {
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">` + body + "</svg>\n"
}

var (
	// icon is the mark on its rounded tile: the favicon and the app's own logo.
	icon = svg(`<rect width="32" height="32" rx="7" fill="` + accent + `"/>` + glyph)
	// favicon swaps in the lifted blue when the browser chrome is dark.
	favicon = svg(`<style>@media (prefers-color-scheme: dark){.tile{fill:` + accentDark + `}}</style>` +
		`<rect class="tile" width="32" height="32" rx="7" fill="` + accent + `"/>` + glyph)
	// square fills the whole canvas: iOS rounds its corners itself.
	square = svg(`<rect width="32" height="32" fill="` + accent + `"/>` + glyph)
	// maskable shrinks the glyph into the central safe circle (radius 40%)
	// that an Android launcher keeps whatever shape it cuts the icon to.
	maskable = svg(`<rect width="32" height="32" fill="` + accent + `"/>` +
		`<g transform="translate(16 16) scale(0.72) translate(-16 -16)">` + glyph + `</g>`)
)

func main() {
	write("static/icon.svg", icon)
	write("static/favicon.svg", favicon)
	tmp, err := os.MkdirTemp("", "netis-favicons")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(tmp)
	write(tmp+"/square.svg", square)
	write(tmp+"/maskable.svg", maskable)

	png("static/icon.svg", 192, "static/icon-192.png")
	png("static/icon.svg", 512, "static/icon-512.png")
	png(tmp+"/maskable.svg", 512, "static/icon-maskable-512.png")
	png(tmp+"/square.svg", 180, "static/apple-touch-icon.png")
	png("static/icon.svg", 16, tmp+"/16.png")
	png("static/icon.svg", 32, tmp+"/32.png")
	run("magick", tmp+"/16.png", tmp+"/32.png", "static/favicon.ico")
}

func write(path, s string) {
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		fail(err)
	}
}

func png(src string, size int, dst string) {
	n := fmt.Sprint(size)
	run("rsvg-convert", "-w", n, "-h", n, "-o", dst, src)
	// Drop timestamps and other chunks so a rerun gives identical bytes.
	run("magick", dst, "-strip", "-define", "png:exclude-chunks=date,time", dst)
}

func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fail(fmt.Errorf("%s: %w", name, err))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen_favicons:", err)
	os.Exit(1)
}

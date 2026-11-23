// Package uirefs embeds the web UI that the binary serves at /.
package uirefs

import "embed"

// WebFS holds the files of web/ (HTML, CSS, JS and fonts, no build step).
//
//go:embed web
var WebFS embed.FS

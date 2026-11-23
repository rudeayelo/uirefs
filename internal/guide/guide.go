// Package guide embeds the usage guide served by the MCP server.
package guide

import _ "embed"

//go:embed guide.md
var Markdown string

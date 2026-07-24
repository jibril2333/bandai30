// Package bandai30 holds the embed declaration for the web SPA at the
// project root so cmd/* can import it. The package itself has no logic.
package bandai30

import "embed"

//go:embed all:web
var WebFS embed.FS

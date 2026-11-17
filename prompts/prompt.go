// Package prompts holds versioned model instructions outside the scoring code.
package prompts

import _ "embed"

// Version is the cache key for the posting extraction prompt.
const Version = "extract-posting-v1"

//go:embed extract-posting-v1.md
var Text string

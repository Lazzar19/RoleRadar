// Package pipeline turns a board snapshot or alert email into stored jobs and scores match tasks.
// Fetch stays outside this package. A failed fetch never reaches Apply, so it cannot close jobs.
package pipeline

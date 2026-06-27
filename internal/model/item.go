// Package model defines the core Item type shared across the application.
package model

import "time"

// Item represents a single piece of content pushed to the wall.
type Item struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Tags      []string       `json:"tags"`
	Pinned    bool           `json:"pinned"`
	Meta      map[string]any `json:"meta"`
	Body      string         `json:"body"`
	Raw       string         `json:"raw,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// Known content types. The frontend uses a registry; unknown types fall back to "note".
const (
	TypeNote       = "note"
	TypePaper      = "paper"
	TypeLink       = "link"
	TypeImage      = "image"
	TypeStockChart = "stock-chart"
)

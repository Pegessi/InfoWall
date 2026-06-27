package parser

import (
	"strings"
	"testing"
)

func TestParseFullFrontmatter(t *testing.T) {
	raw := []byte(`---
type: paper
title: Attention Is All You Need
tags: [ml, transformer]
pinned: true
authors: ["Vaswani et al."]
---
# Abstract
We propose a new architecture...`)

	it, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if it.Type != "paper" {
		t.Errorf("type = %q, want paper", it.Type)
	}
	if it.Title != "Attention Is All You Need" {
		t.Errorf("title = %q", it.Title)
	}
	if it.Pinned != true {
		t.Error("pinned should be true")
	}
	if len(it.Tags) != 2 || it.Tags[0] != "ml" || it.Tags[1] != "transformer" {
		t.Errorf("tags = %v", it.Tags)
	}
	if !strings.Contains(it.Body, "We propose a new architecture") {
		t.Errorf("body missing content, got %q", it.Body)
	}
	if authors, ok := it.Meta["authors"].([]interface{}); !ok || len(authors) != 1 {
		t.Errorf("meta.authors = %v", it.Meta["authors"])
	}
	if it.ID == "" {
		t.Error("id should be set")
	}
	if it.CreatedAt.IsZero() {
		t.Error("created_at should be set")
	}
}

func TestParseNoFrontmatterDefaultsToNote(t *testing.T) {
	raw := []byte(`# Hello World
This is a **simple** note.`)
	it, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if it.Type != "note" {
		t.Errorf("default type = %q, want note", it.Type)
	}
	if it.Title != "Hello World" {
		t.Errorf("title should be derived from H1, got %q", it.Title)
	}
	if it.Pinned {
		t.Error("pinned should default to false")
	}
	if !strings.Contains(it.Body, "simple") {
		t.Errorf("body: %q", it.Body)
	}
}

func TestParseTitleFromH1(t *testing.T) {
	raw := []byte(`---
type: note
---
# My Custom Title

Body content here.`)
	it, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "My Custom Title" {
		t.Errorf("title = %q", it.Title)
	}
}

func TestParseTitleFromFirstLineWhenNoH1(t *testing.T) {
	raw := []byte(`Just a regular first line of content
that continues below.`)
	it, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Just a regular first line of content" {
		t.Errorf("title = %q", it.Title)
	}
}

func TestParseTitleTruncatedTo60(t *testing.T) {
	long := strings.Repeat("a", 100)
	it, err := Parse([]byte(long))
	if err != nil {
		t.Fatal(err)
	}
	// 60 'a' + "…" (3 bytes in UTF-8) = 63 bytes
	if len(it.Title) != 63 {
		t.Errorf("title length = %d, want 63; title=%q", len(it.Title), it.Title)
	}
	if !strings.HasSuffix(it.Title, "…") {
		t.Error("title should end with ellipsis")
	}
}

func TestParsePinnedFromFrontmatter(t *testing.T) {
	raw := []byte("---\npinned: true\n---\npinned body")
	it, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !it.Pinned {
		t.Error("expected pinned=true")
	}
}

func TestParseTagsCommaSeparated(t *testing.T) {
	raw := []byte("---\ntags: a, b, c\n---\nbody")
	it, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(it.Tags) != 3 || it.Tags[0] != "a" || it.Tags[1] != "b" || it.Tags[2] != "c" {
		t.Errorf("tags = %v", it.Tags)
	}
}

func TestParseTagsYAMLList(t *testing.T) {
	raw := []byte("---\ntags:\n  - x\n  - y\n---\nbody")
	it, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(it.Tags) != 2 {
		t.Errorf("tags = %v", it.Tags)
	}
}

func TestParseCreatedAtOverride(t *testing.T) {
	raw := []byte("---\ncreated_at: 2024-01-15T10:30:00Z\n---\nbody")
	it, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if it.CreatedAt.Year() != 2024 || it.CreatedAt.Month() != 1 || it.CreatedAt.Day() != 15 {
		t.Errorf("created_at = %v", it.CreatedAt)
	}
}

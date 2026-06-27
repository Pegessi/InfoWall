---
title: Weekend Project Notes
tags: [ideas, side-project]
---

I spent the morning thinking through the infowall idea. A few things keep coming
back to me:

1. Knowledge work is mostly about *recall at the right moment* — not storage.
2. Existing tools (Notion, Obsidian, Roam) bias toward writing, not glancing.
3. A wall of cards that I can glance at while sipping coffee is closer to what
   I actually want than a hierarchical tree of documents.

## Rough sketch

The core loop should be:

- Push a markdown blob from anywhere (CLI, curl, iOS shortcut).
- It lands in reverse-chronological order on the wall.
- Different "types" render differently: papers show authors + abstract, links
  show a card, stocks render a chart inline.

```go
type Item struct {
    ID    string
    Type  string        // note, paper, link, image, stock-chart
    Title string
    Tags  []string
    Body  string        // markdown
    Meta  map[string]any
}
```

Next steps: try a one-week dogfood. If I push at least 3 items/day for a week
and it feels useful, keep going. Otherwise, archive it.

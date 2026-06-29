export const CUSTOM_PRESET_ID = "custom";
export const COLUMN_MIN_WIDTH = 280;
export const COLUMN_MAX_WIDTH = 720;
export const DEFAULT_COLUMN_WIDTH = 360;
export const PANEL_MIN_HEIGHT = 360;
export const PANEL_MAX_HEIGHT = 960;
export const DEFAULT_PANEL_HEIGHT = 600;

export type FeedLayoutMode = "single" | "topics";

export interface TopicColumnPreference {
  id: string;
  width: number;
  height: number;
}

export interface FeedLayoutState {
  version: 1;
  mode: FeedLayoutMode;
  presetId: string;
  columns: TopicColumnPreference[];
}

export interface FeedLayoutPreset {
  id: string;
  label: string;
  mode: FeedLayoutMode;
  columns: TopicColumnPreference[];
}

export const TOPIC_ORDER = ["note", "paper", "link", "image", "stock-chart"];

const TOPIC_LABELS: Record<string, string> = {
  note: "Notes",
  paper: "Papers",
  link: "Links",
  image: "Images",
  "stock-chart": "Charts",
};

export const FEED_LAYOUT_PRESETS: FeedLayoutPreset[] = [
  {
    id: "timeline",
    label: "Topic stack",
    mode: "single",
    columns: [
      { id: "note", width: 360, height: 600 },
      { id: "paper", width: 420, height: 600 },
      { id: "link", width: 380, height: 600 },
      { id: "image", width: 420, height: 600 },
      { id: "stock-chart", width: 460, height: 600 },
    ],
  },
  {
    id: "balanced",
    label: "Balanced",
    mode: "topics",
    columns: [
      { id: "note", width: 360, height: 600 },
      { id: "paper", width: 420, height: 600 },
      { id: "link", width: 380, height: 600 },
      { id: "image", width: 420, height: 600 },
      { id: "stock-chart", width: 460, height: 600 },
    ],
  },
  {
    id: "research",
    label: "Research",
    mode: "topics",
    columns: [
      { id: "paper", width: 520, height: 700 },
      { id: "link", width: 420, height: 640 },
      { id: "note", width: 360, height: 600 },
      { id: "stock-chart", width: 420, height: 640 },
      { id: "image", width: 360, height: 600 },
    ],
  },
  {
    id: "market",
    label: "Market",
    mode: "topics",
    columns: [
      { id: "stock-chart", width: 600, height: 720 },
      { id: "link", width: 400, height: 600 },
      { id: "note", width: 340, height: 560 },
      { id: "paper", width: 380, height: 600 },
      { id: "image", width: 360, height: 560 },
    ],
  },
  {
    id: "compact",
    label: "Compact",
    mode: "topics",
    columns: [
      { id: "note", width: 300, height: 440 },
      { id: "paper", width: 320, height: 440 },
      { id: "link", width: 320, height: 440 },
      { id: "image", width: 320, height: 440 },
      { id: "stock-chart", width: 360, height: 480 },
    ],
  },
];

export const DEFAULT_FEED_LAYOUT = layoutFromPreset(
  FEED_LAYOUT_PRESETS.find((preset) => preset.id === "timeline") ??
    FEED_LAYOUT_PRESETS[0]
);

export function clampColumnWidth(width: number): number {
  if (!Number.isFinite(width)) return DEFAULT_COLUMN_WIDTH;
  return Math.min(COLUMN_MAX_WIDTH, Math.max(COLUMN_MIN_WIDTH, Math.round(width)));
}

export function clampPanelHeight(height: number): number {
  if (!Number.isFinite(height)) return DEFAULT_PANEL_HEIGHT;
  return Math.min(PANEL_MAX_HEIGHT, Math.max(PANEL_MIN_HEIGHT, Math.round(height)));
}

export function formatTopicLabel(topicId: string): string {
  if (TOPIC_LABELS[topicId]) return TOPIC_LABELS[topicId];
  return topicId
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

export function layoutFromPreset(preset: FeedLayoutPreset): FeedLayoutState {
  return {
    version: 1,
    mode: preset.mode,
    presetId: preset.id,
    columns: preset.columns.map((column) => ({
      id: column.id,
      width: clampColumnWidth(column.width),
      height: clampPanelHeight(column.height),
    })),
  };
}

export function sortTopicIds(topicIds: string[]): string[] {
  return [...topicIds].sort((a, b) => {
    const aIndex = TOPIC_ORDER.indexOf(a);
    const bIndex = TOPIC_ORDER.indexOf(b);
    if (aIndex !== -1 || bIndex !== -1) {
      if (aIndex === -1) return 1;
      if (bIndex === -1) return -1;
      return aIndex - bIndex;
    }
    return formatTopicLabel(a).localeCompare(formatTopicLabel(b));
  });
}

export function resolveLayoutColumns(
  layout: FeedLayoutState,
  topicIds: string[]
): TopicColumnPreference[] {
  const availableTopics = new Set(topicIds);
  const resolved = layout.columns
    .filter((column) => availableTopics.has(column.id))
    .map((column) => ({
      id: column.id,
      width: clampColumnWidth(column.width),
      height: clampPanelHeight(column.height),
    }));
  const seen = new Set(resolved.map((column) => column.id));

  for (const topicId of sortTopicIds(topicIds)) {
    if (seen.has(topicId)) continue;
    resolved.push({
      id: topicId,
      width: defaultWidthForTopic(topicId),
      height: defaultHeightForTopic(topicId),
    });
  }

  return resolved;
}

export function defaultWidthForTopic(topicId: string): number {
  const balanced = FEED_LAYOUT_PRESETS.find((preset) => preset.id === "balanced");
  const column = balanced?.columns.find((entry) => entry.id === topicId);
  return column?.width ?? DEFAULT_COLUMN_WIDTH;
}

export function defaultHeightForTopic(topicId: string): number {
  const balanced = FEED_LAYOUT_PRESETS.find((preset) => preset.id === "balanced");
  const column = balanced?.columns.find((entry) => entry.id === topicId);
  return column?.height ?? DEFAULT_PANEL_HEIGHT;
}

export function parseStoredLayout(raw: string | null): FeedLayoutState | null {
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as Partial<FeedLayoutState>;
    const mode: FeedLayoutMode = parsed.mode === "single" ? "single" : "topics";
    const columns = Array.isArray(parsed.columns)
      ? parsed.columns
          .filter(
            (column): column is TopicColumnPreference =>
              typeof column?.id === "string" && typeof column?.width === "number"
          )
          .map((column) => ({
            id: column.id,
            width: clampColumnWidth(column.width),
            height: clampPanelHeight(
              typeof column.height === "number"
                ? column.height
                : defaultHeightForTopic(column.id)
            ),
          }))
      : [];

    return {
      version: 1,
      mode,
      presetId:
        typeof parsed.presetId === "string" ? parsed.presetId : CUSTOM_PRESET_ID,
      columns: columns.length > 0 ? columns : DEFAULT_FEED_LAYOUT.columns,
    };
  } catch {
    return null;
  }
}

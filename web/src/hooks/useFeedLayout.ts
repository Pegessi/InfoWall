import { useCallback, useEffect, useMemo, useState } from "react";
import {
  CUSTOM_PRESET_ID,
  DEFAULT_FEED_LAYOUT,
  FEED_LAYOUT_PRESETS,
  type FeedLayoutMode,
  type FeedLayoutState,
  clampColumnWidth,
  clampPanelHeight,
  layoutFromPreset,
  parseStoredLayout,
  resolveLayoutColumns,
} from "@/lib/feedLayout";

const STORAGE_KEY = "infowall.feed.layout.v1";

function loadInitialLayout(): FeedLayoutState {
  if (typeof window === "undefined") return DEFAULT_FEED_LAYOUT;
  return parseStoredLayout(window.localStorage.getItem(STORAGE_KEY)) ?? DEFAULT_FEED_LAYOUT;
}

function reorderColumns(
  columns: FeedLayoutState["columns"],
  sourceId: string,
  targetId: string
) {
  if (sourceId === targetId) return columns;
  const next = [...columns];
  const sourceIndex = next.findIndex((column) => column.id === sourceId);
  const targetIndex = next.findIndex((column) => column.id === targetId);
  if (sourceIndex === -1 || targetIndex === -1) return columns;
  const [source] = next.splice(sourceIndex, 1);
  next.splice(targetIndex, 0, source);
  return next;
}

export function useFeedLayout(topicIds: string[]) {
  const [layout, setLayout] = useState<FeedLayoutState>(loadInitialLayout);
  const topicKey = topicIds.join("\u0000");

  useEffect(() => {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(layout));
  }, [layout]);

  const columns = useMemo(
    () => resolveLayoutColumns(layout, topicIds),
    [layout, topicKey]
  );

  const applyPreset = useCallback((presetId: string) => {
    const preset = FEED_LAYOUT_PRESETS.find((entry) => entry.id === presetId);
    if (!preset) return;
    setLayout(layoutFromPreset(preset));
  }, []);

  const setMode = useCallback((mode: FeedLayoutMode) => {
    setLayout((previous) => {
      if (previous.mode === mode) return previous;
      if (mode === "single") {
        return { ...previous, mode: "single", presetId: "timeline" };
      }
      return {
        ...previous,
        mode: "topics",
        presetId: previous.presetId === "timeline" ? "balanced" : previous.presetId,
        columns:
          previous.columns.length > 0 ? previous.columns : DEFAULT_FEED_LAYOUT.columns,
      };
    });
  }, []);

  const moveColumn = useCallback(
    (sourceId: string, targetId: string) => {
      setLayout((previous) => ({
        ...previous,
        presetId: CUSTOM_PRESET_ID,
        columns: reorderColumns(
          resolveLayoutColumns(previous, topicIds),
          sourceId,
          targetId
        ),
      }));
    },
    [topicKey]
  );

  const moveColumnByStep = useCallback(
    (columnId: string, direction: -1 | 1) => {
      setLayout((previous) => {
        const resolved = resolveLayoutColumns(previous, topicIds);
        const index = resolved.findIndex((column) => column.id === columnId);
        const target = resolved[index + direction];
        if (!target) return previous;
        return {
          ...previous,
          presetId: CUSTOM_PRESET_ID,
          columns: reorderColumns(resolved, columnId, target.id),
        };
      });
    },
    [topicKey]
  );

  const setColumnWidth = useCallback(
    (columnId: string, width: number) => {
      setLayout((previous) => ({
        ...previous,
        presetId: CUSTOM_PRESET_ID,
        columns: resolveLayoutColumns(previous, topicIds).map((column) =>
          column.id === columnId
            ? { ...column, width: clampColumnWidth(width) }
            : column
        ),
      }));
    },
    [topicKey]
  );

  const setColumnHeight = useCallback(
    (columnId: string, height: number) => {
      setLayout((previous) => ({
        ...previous,
        presetId: CUSTOM_PRESET_ID,
        columns: resolveLayoutColumns(previous, topicIds).map((column) =>
          column.id === columnId
            ? { ...column, height: clampPanelHeight(height) }
            : column
        ),
      }));
    },
    [topicKey]
  );

  return {
    layout,
    columns,
    presets: FEED_LAYOUT_PRESETS,
    applyPreset,
    setMode,
    moveColumn,
    moveColumnByStep,
    setColumnWidth,
    setColumnHeight,
  };
}

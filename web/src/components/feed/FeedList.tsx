import {
  ArrowLeft,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronUp,
  Columns3,
  GripVertical,
  Loader2,
  Maximize2,
  MoveHorizontal,
  MoveVertical,
  Rows3,
  SearchX,
  X,
} from "lucide-react";
import {
  useCallback,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type DragEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import { useFeed } from "@/hooks/useFeed";
import { useFeedLayout } from "@/hooks/useFeedLayout";
import {
  PANEL_MAX_HEIGHT,
  PANEL_MIN_HEIGHT,
  CUSTOM_PRESET_ID,
  formatTopicLabel,
  sortTopicIds,
  type FeedLayoutMode,
  type TopicColumnPreference,
} from "@/lib/feedLayout";
import {
  availableTypes as deriveAvailableTypes,
  isFilterActive,
  EMPTY_FEED_FILTER,
  type FeedFilter,
  type PinnedFilter,
} from "@/lib/feedFilter";
import type { Item } from "@/lib/types";
import { cn } from "@/lib/utils";
import { ICON_MAP } from "./iconMap";
import { ItemCard } from "./ItemCard";
import { EmptyState } from "./EmptyState";
import { FeedFilterBar } from "./FeedFilterBar";
import { ConnectionStatus } from "./ConnectionStatus";

const PANEL_HEIGHT_STEP = 80;
const PANEL_VIEWPORT_FILL_HEIGHT = "calc(100vh - 8.5rem)";

function topicPanelStyle(column: TopicColumnPreference): CSSProperties {
  return {
    height: `min(${PANEL_MAX_HEIGHT}px, max(${column.height}px, ${PANEL_VIEWPORT_FILL_HEIGHT}))`,
  };
}

export function FeedList() {
  const [filter, setFilter] = useState<FeedFilter>(EMPTY_FEED_FILTER);
  const {
    items,
    loading,
    error,
    hasMore,
    loadingMore,
    connection,
    loadMore,
    pinItem,
    deleteItem,
    retry,
  } = useFeed(filter);

  const filterActive = isFilterActive(filter);
  // items are already filtered server-side (full history); the type dropdown is
  // sourced from the loaded items so it reflects topics currently in view.
  const typeOptions = useMemo(() => deriveAvailableTypes(items), [items]);
  // No results for the ACTIVE query once loading settles (server returned none).
  const noResults = !loading && filterActive && items.length === 0;
  // Distinguish a genuinely empty wall (no filter, no items) from a filtered
  // zero-result set so the first-run guidance only shows on a truly empty wall.
  const emptyWall = !loading && !filterActive && items.length === 0 && !error;

  const groupedItems = useMemo(
    () => groupItemsByTopic(items),
    [items]
  );
  // Drive the layout from the loaded topic set so column order/width/height
  // persistence stays coherent. Panels only render topics that have items.
  const topicIds = useMemo(
    () => sortTopicIds(deriveAvailableTypes(items)),
    [items]
  );
  const {
    layout,
    columns,
    presets,
    applyPreset,
    setMode,
    moveColumn,
    moveColumnByStep,
    setColumnWidth,
    setColumnHeight,
  } = useFeedLayout(topicIds);
  const [focusedTopicId, setFocusedTopicId] = useState<string | null>(null);
  const presetValue = presets.some((preset) => preset.id === layout.presetId)
    ? layout.presetId
    : CUSTOM_PRESET_ID;

  const resetFilter = useCallback(() => setFilter(EMPTY_FEED_FILTER), []);

  // The controls (search/filter + layout + connection status) stay visible
  // whenever the wall is not empty OR a filter is active, so an operator can
  // adjust or clear a query that currently matches nothing.
  const showControls = items.length > 0 || filterActive;

  return (
    <div className="space-y-3">
      {loading && items.length === 0 && (
        <>
          {[0, 1, 2].map((i) => (
            <div
              key={i}
              className="rounded-xl bg-[hsl(var(--muted))] h-32 animate-pulse"
            />
          ))}
        </>
      )}

      {emptyWall && <EmptyState />}

      {error && items.length === 0 && (
        <div className="rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--card))] p-6 text-center text-sm text-[hsl(var(--negative))]">
          {error}
        </div>
      )}

      {showControls && (
        <>
          {focusedTopicId ? (
            <TopicFocusView
              topicId={focusedTopicId}
              items={groupedItems.get(focusedTopicId) ?? []}
              onBack={() => setFocusedTopicId(null)}
              onPin={pinItem}
              onDelete={deleteItem}
            />
          ) : (
            <>
              <div className="space-y-2">
                <FeedFilterBar
                  filter={filter}
                  availableTypes={typeOptions}
                  resultCount={items.length}
                  totalCount={items.length}
                  hasMore={hasMore}
                  onQueryChange={(query) =>
                    setFilter((prev) => ({ ...prev, query }))
                  }
                  onTypeChange={(type) =>
                    setFilter((prev) => ({ ...prev, type }))
                  }
                  onPinnedChange={(pinned: PinnedFilter) =>
                    setFilter((prev) => ({ ...prev, pinned }))
                  }
                  onReset={resetFilter}
                />
                {!noResults && (
                  <LayoutControls
                    mode={layout.mode}
                    presetValue={presetValue}
                    showCustomPreset={presetValue === CUSTOM_PRESET_ID}
                    presets={presets}
                    onModeChange={setMode}
                    onPresetChange={applyPreset}
                    status={
                      <ConnectionStatus state={connection} onRetry={retry} />
                    }
                  />
                )}
                {noResults && (
                  <div className="flex justify-end">
                    <ConnectionStatus state={connection} onRetry={retry} />
                  </div>
                )}
              </div>

              {error && (
                <div className="rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--card))] p-3 text-sm text-[hsl(var(--negative))]">
                  {error}
                </div>
              )}

              {noResults ? (
                <NoResultsState
                  filterActive={filterActive}
                  onReset={resetFilter}
                />
              ) : layout.mode === "topics" ? (
                <>
                  <TopicStack
                    className="lg:hidden"
                    columns={columns}
                    groupedItems={groupedItems}
                    onPin={pinItem}
                    onDelete={deleteItem}
                    onColumnHeightChange={setColumnHeight}
                    onOpenTopic={setFocusedTopicId}
                  />
                  <TopicBoard
                    columns={columns}
                    groupedItems={groupedItems}
                    onPin={pinItem}
                    onDelete={deleteItem}
                    onMoveColumn={moveColumn}
                    onMoveColumnByStep={moveColumnByStep}
                    onColumnWidthChange={setColumnWidth}
                    onColumnHeightChange={setColumnHeight}
                    onOpenTopic={setFocusedTopicId}
                  />
                </>
              ) : (
                <TopicStack
                  columns={columns}
                  groupedItems={groupedItems}
                  onPin={pinItem}
                  onDelete={deleteItem}
                  onColumnHeightChange={setColumnHeight}
                  onOpenTopic={setFocusedTopicId}
                />
              )}
            </>
          )}
        </>
      )}

      {hasMore && !focusedTopicId && (
        <div className="pt-2">
          <button
            type="button"
            onClick={() => void loadMore()}
            disabled={loadingMore}
            className="mx-auto block rounded-md border border-[hsl(var(--border))] px-4 py-2 text-sm transition-colors hover:bg-[hsl(var(--muted))] disabled:opacity-60"
          >
            {loadingMore ? (
              <span className="inline-flex items-center gap-2">
                <Loader2 className="h-4 w-4 animate-spin" />
                Loading…
              </span>
            ) : (
              "Load more"
            )}
          </button>
        </div>
      )}

      {items.length > 0 && !hasMore && !loading && !focusedTopicId && (
        <div className="pt-1 text-center text-xs text-[hsl(var(--muted-foreground))]">
          {filterActive
            ? "End of results for this search."
            : "End of history — all items are shown."}
        </div>
      )}
    </div>
  );
}

interface NoResultsStateProps {
  filterActive: boolean;
  onReset: () => void;
}

function NoResultsState({ filterActive, onReset }: NoResultsStateProps) {
  return (
    <div className="flex flex-col items-center gap-3 rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--card))] px-6 py-10 text-center">
      <SearchX
        className="h-6 w-6 text-[hsl(var(--muted-foreground))]"
        strokeWidth={1.75}
      />
      <div className="text-sm text-[hsl(var(--muted-foreground))]">
        No items in the full history match this search.
      </div>
      {filterActive && (
        <button
          type="button"
          onClick={onReset}
          className="inline-flex h-9 items-center gap-1.5 rounded-md border border-[hsl(var(--border))] px-3 text-sm transition-colors hover:bg-[hsl(var(--muted))]"
        >
          <X className="h-3.5 w-3.5" strokeWidth={2} />
          Clear filters
        </button>
      )}
    </div>
  );
}

interface LayoutControlsProps {
  mode: FeedLayoutMode;
  presetValue: string;
  showCustomPreset: boolean;
  presets: ReturnType<typeof useFeedLayout>["presets"];
  onModeChange: (mode: FeedLayoutMode) => void;
  onPresetChange: (presetId: string) => void;
  status?: ReactNode;
}

function LayoutControls({
  mode,
  presetValue,
  showCustomPreset,
  presets,
  onModeChange,
  onPresetChange,
  status,
}: LayoutControlsProps) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="inline-flex items-center gap-3">
        <div className="inline-flex rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--card))] p-1">
        <button
          type="button"
          aria-pressed={mode === "single"}
          aria-label="Topic stack layout"
          title="Topic stack"
          onClick={() => onModeChange("single")}
          className={cn(
            "rounded-md p-2 transition-colors",
            mode === "single"
              ? "bg-[hsl(var(--accent))] text-[hsl(var(--accent-foreground))]"
              : "text-[hsl(var(--muted-foreground))] hover:bg-[hsl(var(--muted))] hover:text-foreground"
          )}
        >
          <Rows3 className="h-4 w-4" strokeWidth={1.75} />
        </button>
        <button
          type="button"
          aria-pressed={mode === "topics"}
          aria-label="Topic columns layout"
          title="Topic columns"
          onClick={() => onModeChange("topics")}
          className={cn(
            "rounded-md p-2 transition-colors",
            mode === "topics"
              ? "bg-[hsl(var(--accent))] text-[hsl(var(--accent-foreground))]"
              : "text-[hsl(var(--muted-foreground))] hover:bg-[hsl(var(--muted))] hover:text-foreground"
          )}
        >
          <Columns3 className="h-4 w-4" strokeWidth={1.75} />
        </button>
      </div>
        {status}
      </div>

      <select
        aria-label="Layout preset"
        value={presetValue}
        onChange={(event) => {
          if (event.target.value !== CUSTOM_PRESET_ID) {
            onPresetChange(event.target.value);
          }
        }}
        className="h-9 rounded-md border border-[hsl(var(--border))] bg-[hsl(var(--card))] px-3 text-sm text-[hsl(var(--foreground))] shadow-sm outline-none transition-colors hover:bg-[hsl(var(--muted))] focus:ring-2 focus:ring-[hsl(var(--ring))]"
      >
        {showCustomPreset && <option value={CUSTOM_PRESET_ID}>Custom</option>}
        {presets.map((preset) => (
          <option key={preset.id} value={preset.id}>
            {preset.label}
          </option>
        ))}
      </select>
    </div>
  );
}

interface TopicStackProps {
  className?: string;
  columns: TopicColumnPreference[];
  groupedItems: Map<string, Item[]>;
  onPin: (id: string, pinned: boolean) => void;
  onDelete: (id: string) => void;
  onColumnHeightChange: (columnId: string, height: number) => void;
  onOpenTopic: (topicId: string) => void;
}

function TopicStack({
  className,
  columns,
  groupedItems,
  onPin,
  onDelete,
  onColumnHeightChange,
  onOpenTopic,
}: TopicStackProps) {
  const visibleColumns = columns.filter(
    (column) => (groupedItems.get(column.id)?.length ?? 0) > 0
  );
  const [resizingHeightColumnId, setResizingHeightColumnId] = useState<
    string | null
  >(null);
  const heightResizeStateRef = useRef<HeightResizeState | null>(null);

  const beginHeightResize = useCallback(
    (column: TopicColumnPreference, event: ReactPointerEvent<HTMLButtonElement>) => {
      event.preventDefault();
      event.stopPropagation();
      heightResizeStateRef.current = {
        id: column.id,
        startY: event.clientY,
        startHeight: column.height,
      };
      setResizingHeightColumnId(column.id);
      document.body.style.cursor = "row-resize";
      document.body.style.userSelect = "none";

      const handlePointerMove = (moveEvent: PointerEvent) => {
        const state = heightResizeStateRef.current;
        if (!state) return;
        onColumnHeightChange(
          state.id,
          state.startHeight + moveEvent.clientY - state.startY
        );
      };

      const stopResize = () => {
        heightResizeStateRef.current = null;
        setResizingHeightColumnId(null);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
        document.removeEventListener("pointermove", handlePointerMove);
        document.removeEventListener("pointerup", stopResize);
        document.removeEventListener("pointercancel", stopResize);
      };

      document.addEventListener("pointermove", handlePointerMove);
      document.addEventListener("pointerup", stopResize);
      document.addEventListener("pointercancel", stopResize);
    },
    [onColumnHeightChange]
  );

  if (visibleColumns.length === 0) return null;

  return (
    <div className={cn("space-y-3", className)} data-feed-layout="topic-stack">
      {visibleColumns.map((column) => {
        const items = groupedItems.get(column.id) ?? [];
        const label = formatTopicLabel(column.id);
        const Icon = ICON_MAP[column.id] ?? ICON_MAP.note;

        return (
          <section
            key={column.id}
            data-topic-panel={column.id}
            style={topicPanelStyle(column)}
            className="group/topic-panel relative flex min-w-0 flex-col rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--card))] shadow-sm"
          >
            <TopicPanelHeader
              column={column}
              label={label}
              itemCount={items.length}
              Icon={Icon}
              resizingHeightColumnId={resizingHeightColumnId}
              onColumnHeightChange={onColumnHeightChange}
              onOpenTopic={onOpenTopic}
              onBeginHeightResize={beginHeightResize}
            />
            <div
              data-topic-scroll={column.id}
              className="min-h-0 flex-1 overflow-y-auto px-2 pb-4 pt-2.5 sm:px-3"
            >
              <div className="space-y-3">
                {items.map((item) => (
                  <ItemCard
                    key={item.id}
                    item={item}
                    onPin={onPin}
                    onDelete={onDelete}
                    showTypeLabel={false}
                  />
                ))}
              </div>
            </div>
            <TopicHeightEdge
              column={column}
              label={label}
              isResizing={resizingHeightColumnId === column.id}
              onBeginHeightResize={beginHeightResize}
            />
          </section>
        );
      })}
    </div>
  );
}

interface TopicPanelHeaderProps {
  column: TopicColumnPreference;
  label: string;
  itemCount: number;
  Icon: typeof ICON_MAP.note;
  resizingHeightColumnId: string | null;
  onColumnHeightChange: (columnId: string, height: number) => void;
  onOpenTopic: (topicId: string) => void;
  onBeginHeightResize: (
    column: TopicColumnPreference,
    event: ReactPointerEvent<HTMLButtonElement>
  ) => void;
}

function TopicPanelHeader({
  column,
  label,
  itemCount,
  Icon,
  resizingHeightColumnId,
  onColumnHeightChange,
  onOpenTopic,
  onBeginHeightResize,
}: TopicPanelHeaderProps) {
  return (
    <div
      role="button"
      tabIndex={0}
      data-topic-header={column.id}
      aria-label={`Open ${label} topic`}
      title={`Open ${label}`}
      onClick={() => onOpenTopic(column.id)}
      onKeyDown={(event) => {
        if (event.key !== "Enter" && event.key !== " ") return;
        event.preventDefault();
        onOpenTopic(column.id);
      }}
      className="group/topic-header relative flex min-h-14 shrink-0 cursor-pointer flex-wrap items-center gap-2 rounded-t-lg border-b border-[hsl(var(--border))] bg-[hsl(var(--background)/0.94)] px-3 py-2 backdrop-blur transition-colors hover:bg-[hsl(var(--muted)/0.75)]"
    >
      <Icon
        className="h-4 w-4 text-[hsl(var(--muted-foreground))]"
        strokeWidth={1.75}
      />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-semibold">{label}</div>
        <div className="text-[11px] tabular-nums text-[hsl(var(--muted-foreground))]">
          {itemCount} {itemCount === 1 ? "item" : "items"}
        </div>
      </div>
      <div
        className="flex flex-wrap items-center justify-end gap-1"
        onKeyDown={(event) => event.stopPropagation()}
      >
        <button
          type="button"
          aria-label={`Open ${label} detail view`}
          title="Open topic"
          onClick={(event) => {
            event.stopPropagation();
            onOpenTopic(column.id);
          }}
          className="rounded p-1 text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground"
        >
          <Maximize2 className="h-4 w-4" strokeWidth={1.75} />
        </button>
        <button
          type="button"
          aria-label={`Decrease ${label} panel height`}
          title={`${Math.max(
            PANEL_MIN_HEIGHT,
            column.height - PANEL_HEIGHT_STEP
          )}px`}
          disabled={column.height <= PANEL_MIN_HEIGHT}
          onClick={(event) => {
            event.stopPropagation();
            onColumnHeightChange(column.id, column.height - PANEL_HEIGHT_STEP);
          }}
          className="rounded p-1 text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground disabled:cursor-not-allowed disabled:opacity-30"
        >
          <ChevronUp className="h-4 w-4" strokeWidth={1.75} />
        </button>
        <button
          type="button"
          aria-label={`${label} panel height ${column.height}px`}
          title={`${column.height}px`}
          onPointerDown={(event) => onBeginHeightResize(column, event)}
          className={cn(
            "inline-flex h-7 min-w-12 cursor-row-resize touch-none items-center justify-center gap-1 rounded border border-[hsl(var(--border))] px-1.5 text-[11px] tabular-nums text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground",
            resizingHeightColumnId === column.id &&
              "bg-[hsl(var(--accent))] text-[hsl(var(--accent-foreground))]"
          )}
        >
          <MoveVertical className="h-3.5 w-3.5" strokeWidth={1.75} />
          {column.height}
        </button>
        <button
          type="button"
          aria-label={`Increase ${label} panel height`}
          title={`${Math.min(
            PANEL_MAX_HEIGHT,
            column.height + PANEL_HEIGHT_STEP
          )}px`}
          disabled={column.height >= PANEL_MAX_HEIGHT}
          onClick={(event) => {
            event.stopPropagation();
            onColumnHeightChange(column.id, column.height + PANEL_HEIGHT_STEP);
          }}
          className="rounded p-1 text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground disabled:cursor-not-allowed disabled:opacity-30"
        >
          <ChevronDown className="h-4 w-4" strokeWidth={1.75} />
        </button>
      </div>
    </div>
  );
}

interface TopicHeightEdgeProps {
  column: TopicColumnPreference;
  label: string;
  isResizing: boolean;
  onBeginHeightResize: (
    column: TopicColumnPreference,
    event: ReactPointerEvent<HTMLButtonElement>
  ) => void;
}

function TopicHeightEdge({
  column,
  label,
  isResizing,
  onBeginHeightResize,
}: TopicHeightEdgeProps) {
  return (
    <button
      type="button"
      aria-label={`Drag ${label} bottom edge to resize panel height`}
      title={`Drag to resize height (${column.height}px)`}
      onPointerDown={(event) => onBeginHeightResize(column, event)}
      className={cn(
        "absolute inset-x-0 -bottom-2 z-10 flex h-4 cursor-row-resize touch-none items-center justify-center rounded-b-lg",
        isResizing && "bg-[hsl(var(--accent)/0.18)]"
      )}
    >
      <span
        className={cn(
          "h-1 w-20 rounded-full bg-[hsl(var(--border))] opacity-70 transition-all group-hover/topic-panel:w-28 hover:bg-[hsl(var(--accent))] hover:opacity-100",
          isResizing && "w-28 bg-[hsl(var(--accent))] opacity-100"
        )}
      />
    </button>
  );
}

interface TopicBoardProps {
  columns: TopicColumnPreference[];
  groupedItems: Map<string, Item[]>;
  onPin: (id: string, pinned: boolean) => void;
  onDelete: (id: string) => void;
  onMoveColumn: (sourceId: string, targetId: string) => void;
  onMoveColumnByStep: (columnId: string, direction: -1 | 1) => void;
  onColumnWidthChange: (columnId: string, width: number) => void;
  onColumnHeightChange: (columnId: string, height: number) => void;
  onOpenTopic: (topicId: string) => void;
}

interface WidthResizeState {
  id: string;
  startX: number;
  startWidth: number;
}

interface HeightResizeState {
  id: string;
  startY: number;
  startHeight: number;
}

function TopicBoard({
  columns,
  groupedItems,
  onPin,
  onDelete,
  onMoveColumn,
  onMoveColumnByStep,
  onColumnWidthChange,
  onColumnHeightChange,
  onOpenTopic,
}: TopicBoardProps) {
  const visibleColumns = columns.filter(
    (column) => (groupedItems.get(column.id)?.length ?? 0) > 0
  );
  const [draggingColumnId, setDraggingColumnId] = useState<string | null>(null);
  const [dropColumnId, setDropColumnId] = useState<string | null>(null);
  const [resizingColumnId, setResizingColumnId] = useState<string | null>(null);
  const [resizingHeightColumnId, setResizingHeightColumnId] = useState<
    string | null
  >(null);
  const widthResizeStateRef = useRef<WidthResizeState | null>(null);
  const heightResizeStateRef = useRef<HeightResizeState | null>(null);

  const beginWidthResize = useCallback(
    (column: TopicColumnPreference, event: ReactPointerEvent<HTMLButtonElement>) => {
      event.preventDefault();
      event.stopPropagation();
      widthResizeStateRef.current = {
        id: column.id,
        startX: event.clientX,
        startWidth: column.width,
      };
      setResizingColumnId(column.id);
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";

      const handlePointerMove = (moveEvent: PointerEvent) => {
        const state = widthResizeStateRef.current;
        if (!state) return;
        onColumnWidthChange(
          state.id,
          state.startWidth + moveEvent.clientX - state.startX
        );
      };

      const stopResize = () => {
        widthResizeStateRef.current = null;
        setResizingColumnId(null);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
        document.removeEventListener("pointermove", handlePointerMove);
        document.removeEventListener("pointerup", stopResize);
        document.removeEventListener("pointercancel", stopResize);
      };

      document.addEventListener("pointermove", handlePointerMove);
      document.addEventListener("pointerup", stopResize);
      document.addEventListener("pointercancel", stopResize);
    },
    [onColumnWidthChange]
  );

  const beginHeightResize = useCallback(
    (column: TopicColumnPreference, event: ReactPointerEvent<HTMLButtonElement>) => {
      event.preventDefault();
      event.stopPropagation();
      heightResizeStateRef.current = {
        id: column.id,
        startY: event.clientY,
        startHeight: column.height,
      };
      setResizingHeightColumnId(column.id);
      document.body.style.cursor = "row-resize";
      document.body.style.userSelect = "none";

      const handlePointerMove = (moveEvent: PointerEvent) => {
        const state = heightResizeStateRef.current;
        if (!state) return;
        onColumnHeightChange(
          state.id,
          state.startHeight + moveEvent.clientY - state.startY
        );
      };

      const stopResize = () => {
        heightResizeStateRef.current = null;
        setResizingHeightColumnId(null);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
        document.removeEventListener("pointermove", handlePointerMove);
        document.removeEventListener("pointerup", stopResize);
        document.removeEventListener("pointercancel", stopResize);
      };

      document.addEventListener("pointermove", handlePointerMove);
      document.addEventListener("pointerup", stopResize);
      document.addEventListener("pointercancel", stopResize);
    },
    [onColumnHeightChange]
  );

  const gridStyle: CSSProperties = {
    gridTemplateColumns: visibleColumns
      .map((column) => `${column.width}px`)
      .join(" "),
  };

  if (visibleColumns.length === 0) return null;

  return (
    <div className="hidden overflow-x-auto pb-2 lg:block" data-feed-layout="topics">
      <div className="grid items-start gap-3" style={gridStyle}>
        {visibleColumns.map((column, index) => {
          const items = groupedItems.get(column.id) ?? [];
          const label = formatTopicLabel(column.id);
          const Icon = ICON_MAP[column.id] ?? ICON_MAP.note;
          const isDragging = draggingColumnId === column.id;
          const isDropTarget =
            dropColumnId === column.id && draggingColumnId !== column.id;

          return (
            <section
              key={column.id}
              data-topic-panel={column.id}
              style={topicPanelStyle(column)}
              className={cn(
                "group/topic-panel relative flex min-w-0 flex-col rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--card))] shadow-sm transition-opacity",
                isDragging && "opacity-50",
                isDropTarget &&
                  "outline outline-2 outline-[hsl(var(--accent)/0.45)]"
              )}
              onDragOver={(event) => {
                event.preventDefault();
                setDropColumnId(column.id);
              }}
              onDragLeave={() => {
                setDropColumnId((current) =>
                  current === column.id ? null : current
                );
              }}
              onDrop={(event) => {
                event.preventDefault();
                const sourceId =
                  event.dataTransfer.getData("text/plain") || draggingColumnId;
                setDraggingColumnId(null);
                setDropColumnId(null);
                if (sourceId) onMoveColumn(sourceId, column.id);
              }}
            >
              <div
                role="button"
                tabIndex={0}
                data-topic-header={column.id}
                aria-label={`Open ${label} topic`}
                title={`Open ${label}`}
                onClick={() => onOpenTopic(column.id)}
                onKeyDown={(event) => {
                  if (event.key !== "Enter" && event.key !== " ") return;
                  event.preventDefault();
                  onOpenTopic(column.id);
                }}
                className="group/topic-header relative flex h-14 shrink-0 cursor-pointer items-center gap-2 rounded-t-lg border-b border-[hsl(var(--border))] bg-[hsl(var(--background)/0.94)] px-2 backdrop-blur transition-colors hover:bg-[hsl(var(--muted)/0.75)]"
              >
                <div
                  role="button"
                  tabIndex={0}
                  draggable
                  aria-label={`Drag ${label} column`}
                  title="Drag"
                  className="cursor-grab rounded p-1.5 text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] active:cursor-grabbing"
                  onClick={(event) => event.stopPropagation()}
                  onKeyDown={(event) => event.stopPropagation()}
                  onDragStart={(event: DragEvent<HTMLDivElement>) => {
                    event.stopPropagation();
                    setDraggingColumnId(column.id);
                    event.dataTransfer.effectAllowed = "move";
                    event.dataTransfer.setData("text/plain", column.id);
                  }}
                  onDragEnd={() => {
                    setDraggingColumnId(null);
                    setDropColumnId(null);
                  }}
                >
                  <GripVertical className="h-4 w-4" strokeWidth={1.75} />
                </div>
                <Icon
                  className="h-4 w-4 text-[hsl(var(--muted-foreground))]"
                  strokeWidth={1.75}
                />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-semibold">{label}</div>
                  <div className="text-[11px] tabular-nums text-[hsl(var(--muted-foreground))]">
                    {items.length} {items.length === 1 ? "item" : "items"}
                  </div>
                </div>
                <div
                  className="flex items-center gap-0.5 pr-1"
                  onKeyDown={(event) => event.stopPropagation()}
                >
                  <button
                    type="button"
                    aria-label={`Move ${label} column left`}
                    title="Move left"
                    disabled={index === 0}
                    onClick={(event) => {
                      event.stopPropagation();
                      onMoveColumnByStep(column.id, -1);
                    }}
                    className="rounded p-1 text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground disabled:cursor-not-allowed disabled:opacity-30"
                  >
                    <ChevronLeft className="h-4 w-4" strokeWidth={1.75} />
                  </button>
                  <button
                    type="button"
                    aria-label={`Move ${label} column right`}
                    title="Move right"
                    disabled={index === visibleColumns.length - 1}
                    onClick={(event) => {
                      event.stopPropagation();
                      onMoveColumnByStep(column.id, 1);
                    }}
                    className="rounded p-1 text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground disabled:cursor-not-allowed disabled:opacity-30"
                  >
                    <ChevronRight className="h-4 w-4" strokeWidth={1.75} />
                  </button>
                  <button
                    type="button"
                    aria-label={`Open ${label} detail view`}
                    title="Open topic"
                    onClick={(event) => {
                      event.stopPropagation();
                      onOpenTopic(column.id);
                    }}
                    className="rounded p-1 text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground"
                  >
                    <Maximize2 className="h-4 w-4" strokeWidth={1.75} />
                  </button>
                  <button
                    type="button"
                    aria-label={`Decrease ${label} panel height`}
                    title={`${Math.max(
                      PANEL_MIN_HEIGHT,
                      column.height - PANEL_HEIGHT_STEP
                    )}px`}
                    disabled={column.height <= PANEL_MIN_HEIGHT}
                    onClick={(event) => {
                      event.stopPropagation();
                      onColumnHeightChange(
                        column.id,
                        column.height - PANEL_HEIGHT_STEP
                      );
                    }}
                    className="rounded p-1 text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground disabled:cursor-not-allowed disabled:opacity-30"
                  >
                    <ChevronUp className="h-4 w-4" strokeWidth={1.75} />
                  </button>
                  <button
                    type="button"
                    aria-label={`${label} panel height ${column.height}px`}
                    title={`${column.height}px`}
                    onPointerDown={(event) => beginHeightResize(column, event)}
                    className={cn(
                      "inline-flex h-7 min-w-12 cursor-row-resize touch-none items-center justify-center gap-1 rounded border border-[hsl(var(--border))] px-1.5 text-[11px] tabular-nums text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground",
                      resizingHeightColumnId === column.id &&
                        "bg-[hsl(var(--accent))] text-[hsl(var(--accent-foreground))]"
                    )}
                  >
                    <MoveVertical className="h-3.5 w-3.5" strokeWidth={1.75} />
                    {column.height}
                  </button>
                  <button
                    type="button"
                    aria-label={`Increase ${label} panel height`}
                    title={`${Math.min(
                      PANEL_MAX_HEIGHT,
                      column.height + PANEL_HEIGHT_STEP
                    )}px`}
                    disabled={column.height >= PANEL_MAX_HEIGHT}
                    onClick={(event) => {
                      event.stopPropagation();
                      onColumnHeightChange(
                        column.id,
                        column.height + PANEL_HEIGHT_STEP
                      );
                    }}
                    className="rounded p-1 text-[hsl(var(--muted-foreground))] transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground disabled:cursor-not-allowed disabled:opacity-30"
                  >
                    <ChevronDown className="h-4 w-4" strokeWidth={1.75} />
                  </button>
                </div>
                <button
                  type="button"
                  aria-label={`Resize ${label} column`}
                  title={`${column.width}px`}
                  onClick={(event) => event.stopPropagation()}
                  onKeyDown={(event) => event.stopPropagation()}
                  onPointerDown={(event) => beginWidthResize(column, event)}
                  className={cn(
                    "absolute -right-2 top-1/2 z-10 flex h-8 w-4 -translate-y-1/2 cursor-col-resize touch-none items-center justify-center rounded-full border border-[hsl(var(--border))] bg-[hsl(var(--card))] text-[hsl(var(--muted-foreground))] shadow-sm transition-colors hover:bg-[hsl(var(--muted))] hover:text-foreground",
                    resizingColumnId === column.id &&
                      "bg-[hsl(var(--accent))] text-[hsl(var(--accent-foreground))]"
                  )}
                >
                  <MoveHorizontal className="h-3.5 w-3.5" strokeWidth={1.75} />
                </button>
              </div>

              <div
                data-topic-scroll={column.id}
                className="min-h-0 flex-1 overflow-y-auto px-2.5 pb-4 pt-2.5"
              >
                <div className="space-y-3">
                  {items.map((item) => (
                    <ItemCard
                      key={item.id}
                      item={item}
                      onPin={onPin}
                      onDelete={onDelete}
                      showTypeLabel={false}
                    />
                  ))}
                </div>
              </div>
              <TopicHeightEdge
                column={column}
                label={label}
                isResizing={resizingHeightColumnId === column.id}
                onBeginHeightResize={beginHeightResize}
              />
            </section>
          );
        })}
      </div>
    </div>
  );
}

interface TopicFocusViewProps {
  topicId: string;
  items: Item[];
  onBack: () => void;
  onPin: (id: string, pinned: boolean) => void;
  onDelete: (id: string) => void;
}

function TopicFocusView({
  topicId,
  items,
  onBack,
  onPin,
  onDelete,
}: TopicFocusViewProps) {
  const label = formatTopicLabel(topicId);
  const Icon = ICON_MAP[topicId] ?? ICON_MAP.note;

  return (
    <section className="w-full">
      <div className="sticky top-14 z-[5] mb-3 flex items-center gap-3 rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--background)/0.94)] px-3 py-2 shadow-sm backdrop-blur">
        <button
          type="button"
          onClick={onBack}
          aria-label="Back to all topics"
          title="Back"
          className="inline-flex h-9 items-center gap-2 rounded-md border border-[hsl(var(--border))] px-3 text-sm transition-colors hover:bg-[hsl(var(--muted))]"
        >
          <ArrowLeft className="h-4 w-4" strokeWidth={1.75} />
          <span>Back</span>
        </button>
        <Icon
          className="h-4 w-4 text-[hsl(var(--muted-foreground))]"
          strokeWidth={1.75}
        />
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-base font-semibold">{label}</h2>
          <div className="text-xs tabular-nums text-[hsl(var(--muted-foreground))]">
            {items.length} {items.length === 1 ? "item" : "items"}
          </div>
        </div>
      </div>

      {items.length > 0 ? (
        <div className="space-y-3">
          {items.map((item) => (
            <ItemCard
              key={item.id}
              item={item}
              onPin={onPin}
              onDelete={onDelete}
              showTypeLabel={false}
            />
          ))}
        </div>
      ) : (
        <div className="rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--card))] p-6 text-center text-sm text-[hsl(var(--muted-foreground))]">
          No items in this topic.
        </div>
      )}
    </section>
  );
}

function groupItemsByTopic(items: Item[]): Map<string, Item[]> {
  const grouped = new Map<string, Item[]>();
  for (const item of items) {
    const topicId = item.type || "note";
    const topicItems = grouped.get(topicId);
    if (topicItems) {
      topicItems.push(item);
    } else {
      grouped.set(topicId, [item]);
    }
  }
  return grouped;
}

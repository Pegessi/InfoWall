import { useEffect, useRef } from "react";
import { createChart, ColorType, type IChartApi, type ISeriesApi, type CandlestickData, type HistogramData, type Time } from "lightweight-charts";
import type { Item } from "@/lib/types";
import { Markdown } from "@/components/markdown/Markdown";

interface OhlcPoint {
  time: string | number;
  open: number;
  high: number;
  low: number;
  close: number;
  volume?: number;
}

// Read a CSS custom property holding space-separated HSL channels (the
// Tailwind v4 convention, e.g. "240 5% 64.9%") and return an rgb()/rgba()
// string. lightweight-charts' color parser does not understand modern hsl()
// syntax, so we convert to rgb here rather than handing it an hsl() string.
function hslVar(name: string, alpha?: number): string {
  const raw = getComputedStyle(document.documentElement)
    .getPropertyValue(name)
    .trim();
  if (!raw) return "";
  const parts = raw.split(/\s+/);
  if (parts.length < 3) return "";
  const h = parseFloat(parts[0]);
  const s = parseFloat(parts[1]) / 100;
  const l = parseFloat(parts[2]) / 100;
  if (Number.isNaN(h) || Number.isNaN(s) || Number.isNaN(l)) return "";

  const c = (1 - Math.abs(2 * l - 1)) * s;
  const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
  const m = l - c / 2;
  let r = 0;
  let g = 0;
  let b = 0;
  if (h < 60) [r, g, b] = [c, x, 0];
  else if (h < 120) [r, g, b] = [x, c, 0];
  else if (h < 180) [r, g, b] = [0, c, x];
  else if (h < 240) [r, g, b] = [0, x, c];
  else if (h < 300) [r, g, b] = [x, 0, c];
  else [r, g, b] = [c, 0, x];

  const R = Math.round((r + m) * 255);
  const G = Math.round((g + m) * 255);
  const B = Math.round((b + m) * 255);
  return alpha === undefined
    ? `rgb(${R}, ${G}, ${B})`
    : `rgba(${R}, ${G}, ${B}, ${alpha})`;
}

export function StockChartRenderer({ item }: { item: Item }) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const candleRef = useRef<ISeriesApi<"Candlestick"> | null>(null);
  const volumeRef = useRef<ISeriesApi<"Histogram"> | null>(null);

  const { meta } = item;
  const symbol: string = meta?.symbol ?? "";
  const timeframe: string | undefined = meta?.timeframe;
  const price: number | undefined = meta?.price ?? meta?.last;
  const changePct: number | undefined = meta?.change_percent ?? meta?.changePct;
  const data: OhlcPoint[] = Array.isArray(meta?.data) ? meta.data : [];

  useEffect(() => {
    if (!containerRef.current) return;
    const mutedFg = hslVar("--muted-foreground");
    const border = hslVar("--border");
    const positive = hslVar("--positive");
    const negative = hslVar("--negative");
    const positiveFaded = hslVar("--positive", 0.5);
    const negativeFaded = hslVar("--negative", 0.5);

    const chart = createChart(containerRef.current, {
      autoSize: true,
      layout: {
        background: { type: ColorType.Solid, color: "transparent" },
        textColor: mutedFg,
        fontSize: 12,
      },
      grid: {
        vertLines: { color: border },
        horzLines: { color: border },
      },
      rightPriceScale: {
        borderColor: border,
      },
      timeScale: {
        borderColor: border,
        timeVisible: true,
      },
      crosshair: {
        vertLine: { color: border },
        horzLine: { color: border },
      },
    });
    chartRef.current = chart;

    const candles = chart.addCandlestickSeries({
      upColor: positive,
      downColor: negative,
      wickUpColor: positive,
      wickDownColor: negative,
      borderVisible: false,
    });
    candleRef.current = candles;

    const hasVolume = data.some((d) => typeof d.volume === "number");
    if (hasVolume) {
      const vol = chart.addHistogramSeries({
        priceFormat: { type: "volume" },
        priceScaleId: "volume",
        color: border,
      });
      chart.priceScale("volume").applyOptions({
        scaleMargins: { top: 0.8, bottom: 0 },
      });
      volumeRef.current = vol;

      vol.setData(
        data.map((d) => ({
          time: d.time as Time,
          value: d.volume ?? 0,
          color: d.close >= d.open ? positiveFaded : negativeFaded,
        })) as HistogramData<Time>[]
      );
    }

    candles.setData(
      data.map((d) => ({
        time: d.time as Time,
        open: d.open,
        high: d.high,
        low: d.low,
        close: d.close,
      })) as CandlestickData<Time>[]
    );

    chart.timeScale().fitContent();

    return () => {
      chart.remove();
      chartRef.current = null;
      candleRef.current = null;
      volumeRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [item.id]);

  const pctText =
    typeof changePct === "number"
      ? `${changePct >= 0 ? "+" : ""}${changePct.toFixed(2)}%`
      : null;
  const pctPositive = (changePct ?? 0) >= 0;

  return (
    <div>
      <div className="mb-3 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="font-mono font-semibold">{symbol}</span>
          {timeframe && (
            <span className="rounded bg-[hsl(var(--muted))] px-1.5 py-0.5 text-xs font-mono text-[hsl(var(--muted-foreground))]">
              {timeframe}
            </span>
          )}
        </div>
        <div className="flex items-baseline gap-2">
          {typeof price === "number" && (
            <span className="font-mono font-medium">
              {price.toLocaleString(undefined, {
                minimumFractionDigits: 2,
                maximumFractionDigits: 2,
              })}
            </span>
          )}
          {pctText && (
            <span
              className={`font-mono text-sm ${
                pctPositive
                  ? "text-[hsl(var(--positive))]"
                  : "text-[hsl(var(--negative))]"
              }`}
            >
              {pctText}
            </span>
          )}
        </div>
      </div>
      <div
        ref={containerRef}
        className="h-80 w-full rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--muted))]/30"
      />
      {item.body && item.body.trim().length > 0 && (
        <div className="mt-3">
          <Markdown>{item.body}</Markdown>
        </div>
      )}
    </div>
  );
}

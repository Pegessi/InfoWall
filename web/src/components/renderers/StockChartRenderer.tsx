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

function hslVar(name: string): string {
  const raw = getComputedStyle(document.documentElement)
    .getPropertyValue(name)
    .trim();
  return raw ? `hsl(${raw})` : "";
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
          color: d.close >= d.open ? positive + "80" : negative + "80",
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

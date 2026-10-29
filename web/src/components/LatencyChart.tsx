import {
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import {
  LOG_FLOOR_MS,
  RUNTIME_COLORS,
  RUNTIME_LABELS,
  RUNTIMES,
  describeSeries,
  faultRateLabel,
  type Pivoted,
} from "../lib/benchData";

interface LatencyChartProps {
  series: Pivoted[];
  logScale: boolean;
  onToggleLog: () => void;
}

/** p95 latency (ms) per fault rate, grouped by runtime. A log-scale toggle is offered because the values span orders of magnitude. */
export function LatencyChart({ series, logScale, onToggleLog }: LatencyChartProps) {
  const data = series.map((p) => ({ ...p, label: faultRateLabel(p.faultRate) }));
  const scale = logScale ? "log" : "linear";
  const domain: [number, "auto"] = logScale ? [LOG_FLOOR_MS, "auto"] : [0, "auto"];
  return (
    <>
      <div className="chart-controls">
        <label>
          <input type="checkbox" checked={logScale} onChange={onToggleLog} /> Log scale
        </label>
      </div>
      <figure
        className="chart"
        role="img"
        aria-label={`p95 latency in milliseconds by fault rate, ${logScale ? "log" : "linear"} scale. ${describeSeries(series, " ms")}`}
      >
        <ResponsiveContainer width="100%" height={280}>
          <BarChart data={data} margin={{ top: 8, right: 16, bottom: 8, left: 8 }}>
            <CartesianGrid stroke="#30363d" />
            <XAxis dataKey="label" stroke="#8b949e" />
            <YAxis scale={scale} domain={domain} unit=" ms" stroke="#8b949e" allowDataOverflow />
            <Tooltip />
            <Legend />
            {RUNTIMES.map((runtime) => (
              <Bar
                key={runtime}
                dataKey={runtime}
                name={RUNTIME_LABELS[runtime]}
                fill={RUNTIME_COLORS[runtime]}
              />
            ))}
          </BarChart>
        </ResponsiveContainer>
      </figure>
    </>
  );
}

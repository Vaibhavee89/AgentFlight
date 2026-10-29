import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import {
  RUNTIME_COLORS,
  RUNTIME_LABELS,
  RUNTIMES,
  describeSeries,
  faultRateLabel,
  type Pivoted,
} from "../lib/benchData";

interface SuccessChartProps {
  series: Pivoted[];
}

/** Success rate (%) against fault rate, one line per runtime. */
export function SuccessChart({ series }: SuccessChartProps) {
  const data = series.map((p) => ({ ...p, label: faultRateLabel(p.faultRate) }));
  return (
    <figure
      className="chart"
      role="img"
      aria-label={`Success rate by fault rate. ${describeSeries(series, "%")}`}
    >
      <ResponsiveContainer width="100%" height={280}>
        <LineChart data={data} margin={{ top: 8, right: 16, bottom: 8, left: 8 }}>
          <CartesianGrid stroke="#30363d" />
          <XAxis dataKey="label" stroke="#8b949e" />
          <YAxis domain={[0, 100]} unit="%" stroke="#8b949e" />
          <Tooltip />
          <Legend />
          {RUNTIMES.map((runtime) => (
            <Line
              key={runtime}
              type="monotone"
              dataKey={runtime}
              name={RUNTIME_LABELS[runtime]}
              stroke={RUNTIME_COLORS[runtime]}
              strokeWidth={2}
              connectNulls
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </figure>
  );
}

const smallNumber = (value: number) =>
  value !== 0 && Math.abs(value) < 0.01
    ? `${value < 0 ? "-" : ""}<0.01`
    : numeric(value);
const number = new Intl.NumberFormat("en-US", { maximumFractionDigits: 2 });
export const numeric = (value: number | undefined) =>
  value === undefined || !Number.isFinite(value)
    ? "—"
    : number.format(Object.is(value, -0) ? 0 : value);
export const ms = (value?: number) =>
  value === undefined || !Number.isFinite(value)
    ? "—"
    : `${value !== 0 && Math.abs(value) < 0.01 ? `${value < 0 ? "-" : ""}<0.01` : numeric(value)} ms`;
export const percent = (value?: number) =>
  value === undefined || !Number.isFinite(value)
    ? "—"
    : `${smallNumber(value * 100)}%`;
export const deltaMS = (value: number) => `${value > 0 ? "+" : ""}${ms(value)}`;
export const deltaPP = (value: number) =>
  `${value > 0 ? "+" : ""}${smallNumber(value * 100)} pp`;
export const shortID = (id: string) =>
  id.replace(/^(history|faultline)-/, "").slice(0, 10);
export const date = (value: string) =>
  new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    timeZoneName: "short",
  }).format(new Date(value));

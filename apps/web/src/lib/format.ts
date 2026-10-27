const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

export type DateFormat =
  | "date"
  | "datetime"
  | "short"
  | "time"
  | "zone";

function pad(n: number): string {
  return n.toString().padStart(2, "0");
}

// Timestamps arrive as RFC3339 with an offset and are rendered in the
// viewer's own timezone: the API speaks absolute instants, so the browser
// decides the clock. An event created at 15:08 in Jakarta is shown as 15:08
// to viewers in Jakarta and at their own local time everywhere else.

function render(iso: string, format: DateFormat, utc: boolean): string {
  const d = new Date(iso);
  const month = utc ? d.getUTCMonth() : d.getMonth();
  const day = utc ? d.getUTCDate() : d.getDate();
  const year = utc ? d.getUTCFullYear() : d.getFullYear();
  const weekday = utc ? d.getUTCDay() : d.getDay();
  const hours = utc ? d.getUTCHours() : d.getHours();
  const minutes = utc ? d.getUTCMinutes() : d.getMinutes();

  switch (format) {
    case "datetime":
      return `${DAYS[weekday]}, ${MONTHS[month]} ${day}, ${year} ${pad(hours)}:${pad(minutes)}`;
    case "short":
      return `${MONTHS[month]} ${day}`;
    case "time":
      return `${pad(hours)}:${pad(minutes)}`;
    case "zone":
      return fmtZone(iso);
    default:
      return `${MONTHS[month]} ${day}, ${year}`;
  }
}

/** "Jun 21, 2026" */
export function fmtDate(iso: string): string {
  return render(iso, "date", false);
}

/** "Sun, Jun 21, 2026 14:30" */
export function fmtDateTime(iso: string): string {
  return render(iso, "datetime", false);
}

/** "Jun 21" */
export function fmtShortDate(iso: string): string {
  return render(iso, "short", false);
}

/** "14:30" */
export function fmtTime(iso: string): string {
  return render(iso, "time", false);
}

/**
 * UTC rendering of a timestamp, used as the server-rendered fallback: the
 * server does not know the visitor's timezone, so the first paint shows UTC and
 * the client corrects it after mount.
 */
export function fmtUTC(iso: string, format: DateFormat = "date"): string {
  return render(iso, format, true);
}

/**
 * Timezone abbreviation of the viewer's own zone, e.g. "GMT+7". Shown next to
 * event and expiry times so nobody has to guess which clock they are reading.
 */
export function fmtZone(iso?: string): string {
  const d = iso ? new Date(iso) : new Date();
  const part = new Intl.DateTimeFormat(undefined, { timeZoneName: "short" })
    .formatToParts(d)
    .find((p) => p.type === "timeZoneName");
  return part?.value ?? "";
}

/** "Rp 120.000" — formats a rupiah amount (already the minor unit for IDR). */
export function fmtIDR(rupiah: number): string {
  return "Rp " + rupiah.toLocaleString("id-ID");
}

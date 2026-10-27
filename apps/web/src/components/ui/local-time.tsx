"use client";

import { useSyncExternalStore } from "react";
import {
  fmtDate,
  fmtDateTime,
  fmtShortDate,
  fmtTime,
  fmtUTC,
  fmtZone,
  type DateFormat,
} from "@/lib/format";

interface LocalTimeProps {
  iso: string;
  format?: DateFormat;
}

// LocalTime renders a timestamp in the visitor's own timezone.
//
// Client components are server-rendered too, and the server does not know the
// visitor's timezone — so the server-rendered markup shows UTC and the client
// swaps in the local clock. useSyncExternalStore with a server snapshot does
// that without a hydration mismatch and without setting state in an effect.
const subscribe = () => () => {};

function localText(iso: string, format: DateFormat): string {
  switch (format) {
    case "datetime":
      return fmtDateTime(iso);
    case "short":
      return fmtShortDate(iso);
    case "time":
      return fmtTime(iso);
    case "zone":
      return fmtZone(iso);
    default:
      return fmtDate(iso);
  }
}

export function LocalTime({ iso, format = "date" }: LocalTimeProps) {
  const text = useSyncExternalStore(
    subscribe,
    () => localText(iso, format),
    () => (format === "zone" ? "" : fmtUTC(iso, format)),
  );
  return <span suppressHydrationWarning>{text}</span>;
}

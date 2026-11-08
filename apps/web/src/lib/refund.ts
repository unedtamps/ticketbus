// Refund presentation helpers. Customers never see the gateway vocabulary —
// only the four states below. Gateway-internal statuses (ACCEPTED, ROUTING,
// LOCKED, REVERSED, REJECTED, ...) are collapsed by the payment service into
// these four before they reach the browser.

import type { CSSProperties } from "react";
import type { RefundStatus } from "@/types";

/** Customer-facing label for a refund state. */
export const REFUND_STATUS_LABEL: Record<RefundStatus, string> = {
  pending: "Menunggu",
  processing: "Sedang diproses",
  succeeded: "Dana sudah dikirim",
  failed: "Perlu ditindaklanjuti",
};

/**
 * Badge style for a refund state. The shared `badge-green`/`badge-red` classes
 * are misnomers across the app (green renders blue, red renders grey), so
 * refunds set their own semantic colours: green for money delivered, red for
 * something the customer must look at.
 */
export function refundBadgeStyle(status: RefundStatus): React.CSSProperties {
  switch (status) {
    case "succeeded":
      return { background: "#E4F6EC", color: "#1B7A48" };
    case "failed":
      return { background: "#FDECEA", color: "#B3362A" };
    case "processing":
      return { background: "#E7EEFF", color: "#2450D9" };
    default:
      return { background: "#FFF4CC", color: "#8A6400" };
  }
}

/**
 * Proper display name for a payout channel. The API stores the gateway code
 * (SHOPEEPAY, BCA); e-wallets have a brand spelling the raw code gets wrong.
 */
const CHANNEL_LABELS: Record<string, string> = {
  BCA: "BCA",
  BNI: "BNI",
  BRI: "BRI",
  MANDIRI: "Mandiri",
  PERMATA: "Permata",
  CIMB: "CIMB Niaga",
  BNC: "BNC",
  GOPAY: "GoPay",
  OVO: "OVO",
  DANA: "DANA",
  SHOPEEPAY: "ShopeePay",
  LINKAJA: "LinkAja",
};

export function channelLabel(code: string | undefined): string {
  if (!code) return "—";
  const key = code.toUpperCase();
  return CHANNEL_LABELS[key] ?? code;
}

/** Last four digits of an account, for a masked destination. */
export function maskedAccount(accountNumber: string | undefined): string {
  if (!accountNumber) return "—";
  return `••••${accountNumber.slice(-4)}`;
}

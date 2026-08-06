"use client";

import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { api } from "@/lib/api-client";
import { fmtIDR } from "@/lib/format";
import { AlertTriangle, ArrowRight, CreditCard, ExternalLink, Loader2, Ticket } from "lucide-react";
import type { TransactionResponse } from "@/types";

export default function PaymentPage() {
  const { txn_id } = useParams<{ txn_id: string }>();
  const router = useRouter();

  const txnQuery = useQuery({
    queryKey: ["payment", txn_id],
    queryFn: () => api.get<TransactionResponse>(`/api/payments/${txn_id}`),
    staleTime: 5 * 1000,
    // Keep this page live while the payment is still open, so it flips to
    // the result once the webhook settles the transaction.
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "pending" || status === "initiated" ? 3000 : false;
    },
  });

  const txn = txnQuery.data;

  // Open the hosted checkout in a NEW tab exactly once. This page stays open
  // and polls the status, so it never redirects or navigates away — the user
  // always has a place to see the payment result.
  const openedRef = useRef(false);
  useEffect(() => {
    if (openedRef.current) return;
    if (!txn) return;
    if ((txn.status === "pending" || txn.status === "initiated") && txn.payment_link_url) {
      openedRef.current = true;
      window.open(txn.payment_link_url, "_blank", "noopener,noreferrer");
    }
  }, [txn]);

  if (txnQuery.isLoading) {
    return (
      <div className="max-w-md mx-auto card text-center py-12">
        <Loader2 className="w-6 h-6 text-[#D9381E] animate-spin mx-auto" />
        <p className="text-sm text-[#4A4541] mt-3">Loading payment details&hellip;</p>
      </div>
    );
  }

  if (txnQuery.isError || !txn) {
    return (
      <div className="max-w-md mx-auto card text-center py-12 space-y-4">
        <Ticket className="w-8 h-8 text-[#D4CEC4] mx-auto" />
        <p className="text-[#8B8580] text-sm">Transaction not found</p>
        <button onClick={() => router.push("/")} className="btn-outline text-sm">
          Back to Events
        </button>
      </div>
    );
  }

  const badgeClass =
    txn.status === "completed" ? "badge-green" :
    txn.status === "expired" ? "badge-red" :
    txn.status === "pending" || txn.status === "initiated" ? "badge-yellow" : "badge-ink";

  return (
    <div className="max-w-md mx-auto space-y-4">
      <div className="text-center mb-6">
        <div className="inline-flex items-center justify-center w-12 h-12 rounded-xl bg-[#D9381E]/6 mb-4">
          <CreditCard className="w-6 h-6 text-[#D9381E]" />
        </div>
        <p className="font-[family-name:var(--font-display)] text-3xl text-[#1A1817] mb-2">Payment</p>
        <p className="text-[#8B8580] text-sm font-mono">
          Txn: {txn.id.slice(0, 12)}&hellip;
        </p>
      </div>

      <div className="card space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-sm text-[#8B8580]">Amount</span>
          <span className="font-[family-name:var(--font-display)] text-xl text-[#1A1817]">
            {fmtIDR(txn.amount_rupiah)}
          </span>
        </div>
        <div className="flex items-center justify-between">
          <span className="text-sm text-[#8B8580]">Currency</span>
          <span className="text-sm font-medium text-[#1A1817]">{txn.currency}</span>
        </div>
        <div className="flex items-center justify-between">
          <span className="text-sm text-[#8B8580]">Status</span>
          <span className={`badge ${badgeClass}`}>{txn.status}</span>
        </div>
        {txn.payment_link_url && (
          <div className="border-t border-dashed border-[#E8E3DC] pt-3">
            <span className="text-sm text-[#8B8580] block mb-1">Payment URL</span>
            <a
              href={txn.payment_link_url}
              target="_blank"
              rel="noopener noreferrer"
              className="text-xs text-[#1A5DB8] font-mono break-all hover:underline inline-flex items-center gap-1"
            >
              {txn.payment_link_url}
              <ExternalLink className="w-3 h-3 flex-shrink-0" />
            </a>
          </div>
        )}
      </div>

      {txn.status === "completed" && (
        <div className="space-y-3">
          <div className="card flex items-center gap-3 text-sm text-[#2D7A46] border-[#2D7A46]/20">
            <CreditCard className="w-4 h-4" />
            <span>Payment successful</span>
          </div>
          <Link
            href={`/confirmation/${txn.booking_id}`}
            className="btn-accent w-full text-center flex items-center justify-center gap-1.5"
          >
            View Confirmation <ArrowRight className="w-4 h-4" />
          </Link>
        </div>
      )}

      {txn.status === "expired" && (
        <div className="card flex items-center gap-3 text-sm text-[#D9381E] border-[#FECACA]">
          <AlertTriangle className="w-4 h-4" />
          <span>Payment expired. Please make a new reservation.</span>
        </div>
      )}

      {(txn.status === "pending" || txn.status === "initiated") && (
        <div className="card text-center py-6">
          <Loader2 className="w-6 h-6 text-[#D9381E] animate-spin mx-auto" />
          <p className="text-sm text-[#4A4541] mt-3">
            {txn.status === "pending"
              ? "Waiting for payment&hellip;"
              : "Starting payment&hellip;"}
          </p>
          <p className="text-xs text-[#8B8580] mt-2">
            Complete the payment in the tab that just opened. If it didn&apos;t
            open, use the button below — this page updates automatically.
          </p>
          {txn.payment_link_url && (
            <a
              href={txn.payment_link_url}
              target="_blank"
              rel="noopener noreferrer"
              className="btn-accent text-sm mt-4"
            >
              Open Payment Page <ExternalLink className="w-3.5 h-3.5" />
            </a>
          )}
        </div>
      )}
    </div>
  );
}

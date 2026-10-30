"use client";

import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { api } from "@/lib/api-client";
import { fmtIDR } from "@/lib/format";
import {
  AlertTriangle,
  ArrowRight,
  CreditCard,
  ExternalLink,
  Loader2,
  RefreshCw,
  Timer,
} from "lucide-react";
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
        <Loader2 className="w-6 h-6 text-[#2450D9] animate-spin mx-auto" />
        <p className="text-sm text-[#6E7B96] mt-3">Loading payment details&hellip;</p>
      </div>
    );
  }

  if (txnQuery.isError || !txn) {
    return (
      <div className="max-w-md mx-auto card text-center py-12 space-y-4">
        <CreditCard className="w-8 h-8 text-[#C9D4E8] mx-auto" />
        <p className="text-[#6E7B96] text-sm">Transaction not found</p>
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
        <div className="icon-tile mx-auto mb-4">
          <CreditCard className="w-5 h-5" />
        </div>
        <p className="font-[family-name:var(--font-display)] text-3xl text-[#16224A] mb-2">Payment</p>
        <p className="text-[#6E7B96] text-sm">
          Txn: {txn.id.slice(0, 12)}&hellip;
        </p>
      </div>

      <div className="card space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-sm text-[#6E7B96]">Amount</span>
          <span className="font-[family-name:var(--font-display)] text-xl text-[#16224A]">
            {fmtIDR(txn.amount_rupiah)}
          </span>
        </div>
        <div className="flex items-center justify-between">
          <span className="text-sm text-[#6E7B96]">Currency</span>
          <span className="text-sm font-bold text-[#16224A]">{txn.currency}</span>
        </div>
        <div className="flex items-center justify-between">
          <span className="text-sm text-[#6E7B96]">Status</span>
          <span className={`badge ${badgeClass}`}>{txn.status}</span>
        </div>
        {txn.payment_link_url && (
          <div className="border-t border-[#E4EAF4] pt-3">
            <span className="text-sm text-[#6E7B96] block mb-1">Payment URL</span>
            <a
              href={txn.payment_link_url}
              target="_blank"
              rel="noopener noreferrer"
              className="text-xs text-[#2450D9] font-semibold break-all hover:underline inline-flex items-center gap-1"
            >
              {txn.payment_link_url}
              <ExternalLink className="w-3 h-3 flex-shrink-0" />
            </a>
          </div>
        )}
      </div>

      {txn.status === "completed" && (
        <div className="space-y-3">
          <div className="card flex items-center gap-3 text-sm font-semibold text-[#2450D9] bg-[#E7EEFF] border-[#E7EEFF]">
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
        <div className="card flex items-center gap-3 text-sm font-semibold text-[#8A6400] bg-[#FFF4CC] border-[#FFF4CC]">
          <AlertTriangle className="w-4 h-4" />
          <span>Payment expired. Please make a new reservation.</span>
        </div>
      )}

      {(txn.status === "pending" || txn.status === "initiated") && (
        <div className="card text-center py-6 space-y-4">
          <Loader2 className="w-6 h-6 text-[#2450D9] animate-spin mx-auto" />
          <div>
            <p className="text-sm font-bold text-[#16224A]">
              {txn.status === "pending"
                ? "Menunggu pembayaran…"
                : "Starting payment…"}
            </p>
            <p className="text-xs text-[#6E7B96] mt-2 leading-relaxed">
              Complete the payment in the tab that just opened. If it
              didn&apos;t open, use the button below — this page updates
              automatically.
            </p>
          </div>
          {txn.payment_link_url && (
            <a
              href={txn.payment_link_url}
              target="_blank"
              rel="noopener noreferrer"
              className="btn-accent text-sm"
            >
              Buka Halaman Pembayaran <ExternalLink className="w-3.5 h-3.5" />
            </a>
          )}
          <p className="flex items-center justify-center gap-1.5 text-[11px] font-semibold text-[#8A6400] bg-[#FFF4CC] rounded-lg px-3 py-2">
            <Timer className="w-3.5 h-3.5" />
            Kursi masih ditahan sampai pembayaran dikonfirmasi
          </p>
          <p className="flex items-center justify-center gap-1.5 text-[11px] text-[#6E7B96]">
            <RefreshCw className="w-3 h-3" />
            Status di-polling tiap 3 detik — webhook yang menentukan hasil akhir
          </p>
        </div>
      )}
    </div>
  );
}

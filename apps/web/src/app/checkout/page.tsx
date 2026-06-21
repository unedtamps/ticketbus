"use client";

import { useEffect, useState, useRef, useCallback, Suspense } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { api } from "@/lib/api-client";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import { CreditCard, ArrowRight, Loader2, AlertTriangle, Ticket, RotateCw } from "lucide-react";
import type { TransactionResponse } from "@/types";

type Phase = "checkout" | "status" | "completed" | "failed";

function CheckoutForm() {
  const params = useSearchParams();
  const bookingId = params.get("booking_id");
  const { user } = useAuth();
  const router = useRouter();
  const [phase, setPhase] = useState<Phase>("checkout");
  const [txn, setTxn] = useState<TransactionResponse | null>(null);
  const [error, setError] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval>>(undefined);

  const stopPolling = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = undefined;
    }
  }, []);

  useEffect(() => {
    return () => stopPolling();
  }, [stopPolling]);

  // Phase 1: POST /by-booking/{id}/checkout — retry on 404, stop on first 200
  // Phase 2: GET /{id}/status — poll on 404, stop on first 200
  useEffect(() => {
    if (!bookingId || !user) return;
    const MAX_WAIT = 300_000;
    const start = Date.now();
    let cancelled = false;

    setPhase("checkout");

    pollRef.current = setInterval(async () => {
      const now = Date.now();
      if (cancelled) return;

      if (now - start > MAX_WAIT) {
        stopPolling();
        if (!cancelled) {
          setError("Transaction not found. Please try again.");
          toast.error("Transaction timed out after 5 minutes.");
        }
        return;
      }

      try {
        const t = await api.post<TransactionResponse>(`/api/payments/by-booking/${bookingId}/checkout`);
        if (cancelled) return;
        stopPolling();
        setTxn(t);

        if (t.status === "completed") {
          setPhase("completed");
        } else if (t.status === "failed") {
          setPhase("failed");
        } else {
          // Processing — poll status until non-404
          setPhase("status");
          pollRef.current = setInterval(async () => {
            if (cancelled) return;
            try {
              const latest = await api.get<TransactionResponse>(`/api/payments/${t.id}/status`);
              if (cancelled) return;
              // Stop on first success — update phase from actual status
              stopPolling();
              setTxn(latest);
              if (latest.status === "completed") {
                setPhase("completed");
              } else if (latest.status === "failed") {
                setPhase("failed");
              } else {
                setPhase("status");
              }
            } catch {
              // 404 — keep polling
            }
          }, 2000);
        }
      } catch {
        // 404 or network — retry POST next interval tick
      }
    }, 5000);

    return () => { cancelled = true; };
  }, [bookingId, user, stopPolling]);

  async function refreshStatus() {
    if (!txn) return;
    setRefreshing(true);
    try {
      const latest = await api.get<TransactionResponse>(`/api/payments/${txn.id}/status`);
      setTxn(latest);
      if (latest.status === "completed") {
        setPhase("completed");
      } else if (latest.status === "failed") {
        setPhase("failed");
      } else {
        setPhase("status");
      }
    } catch {
      // keep current state
    } finally {
      setRefreshing(false);
    }
  }

  if (!bookingId) {
    return (
      <div className="card text-center py-12 max-w-md mx-auto">
        <AlertTriangle className="w-8 h-8 text-[#D4CEC4] mx-auto mb-3" />
        <p className="text-[#8B8580] text-sm">No booking found. Start from an event page.</p>
      </div>
    );
  }

  return (
    <div className="max-w-md mx-auto">
      <div className="text-center mb-8">
        <div className="inline-flex items-center justify-center w-12 h-12 rounded-xl bg-[#D9381E]/6 mb-4">
          <CreditCard className="w-6 h-6 text-[#D9381E]" />
        </div>
        <p className="font-[family-name:var(--font-display)] text-3xl text-[#1A1817] mb-2">Checkout</p>
        <p className="text-[#8B8580] text-sm font-mono">Booking: {bookingId.slice(0, 12)}&hellip;</p>
      </div>

      {error && (
        <div className="card text-center border-[#FECACA] bg-[#FFF5F5] space-y-3">
          <p className="text-[#D9381E] text-sm">{error}</p>
          <button onClick={() => router.push("/")} className="btn-outline text-sm">Back to Events</button>
        </div>
      )}

      {/* Phase: Checkout — POST retry for eventual consistency */}
      {phase === "checkout" && !error && (
        <div className="card text-center py-8">
          <Loader2 className="w-6 h-6 text-[#D9381E] animate-spin mx-auto" />
          <p className="text-sm text-[#4A4541] mt-3">Starting payment&hellip;</p>
        </div>
      )}

      {txn && (
        <div className="space-y-4">
          <div className="card-stub space-y-3">
            <div className="flex items-center justify-between pb-3 border-b border-dashed border-[#E8E3DC]">
              <span className="text-sm text-[#8B8580]">Amount</span>
              <span className="font-[family-name:var(--font-display)] text-xl text-[#1A1817]">
                ${(txn.amount_cents / 100).toFixed(2)}
              </span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-[#8B8580]">Status</span>
              <span className={`badge ${
                txn.status === "completed" ? "badge-green" :
                txn.status === "failed" ? "badge-red" : "badge-yellow"
              }`}>{txn.status}</span>
            </div>
          </div>

          {/* Status — polling stopped, manual refresh */}
          {phase === "status" && (
            <div className="card flex items-center justify-between text-sm">
              <span className="text-[#B85C1A]">Payment in progress&hellip;</span>
              <button onClick={refreshStatus} disabled={refreshing} className="btn-outline text-sm flex items-center gap-1.5">
                {refreshing ? (
                  <Loader2 className="w-4 h-4 animate-spin" />
                ) : (
                  <RotateCw className="w-4 h-4" />
                )}
                Refresh
              </button>
            </div>
          )}

          {phase === "completed" && (
            <div className="space-y-3">
              <div className="card flex items-center gap-3 text-sm text-[#2D7A46] border-[#2D7A46]/20">
                <CreditCard className="w-4 h-4" />
                <span>Payment successful</span>
              </div>
              <button onClick={() => router.push("/dashboard")} className="btn-accent w-full">
                View Dashboard <ArrowRight className="w-4 h-4" />
              </button>
            </div>
          )}

          {phase === "failed" && (
            <div className="space-y-3">
              <div className="card flex items-center gap-3 text-sm text-[#D9381E] border-[#FECACA]">
                <Ticket className="w-4 h-4" />
                <span>Payment failed. Your reservation may have expired.</span>
              </div>
              <div className="flex gap-3">
                <button onClick={() => router.push("/")} className="btn-outline flex-1">
                  Back to Events
                </button>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export default function CheckoutPage() {
  return (
    <Suspense fallback={
      <div className="max-w-md mx-auto card text-center py-10">
        <Loader2 className="w-6 h-6 text-[#D9381E] animate-spin mx-auto" />
      </div>
    }>
      <CheckoutForm />
    </Suspense>
  );
}

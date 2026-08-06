"use client";

import { useEffect, useState, useRef, useCallback, Suspense } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { api } from "@/lib/api-client";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import {
  CreditCard,
  ArrowRight,
  Loader2,
  AlertTriangle,
  Ticket,
} from "lucide-react";
import type { InitiatePaymentResponse, PaymentStatusResponse } from "@/types";

type Phase = "starting" | "status" | "completed" | "failed";

function formatIDR(cents: number) {
  return "Rp " + cents.toLocaleString("id-ID");
}

function CheckoutForm() {
  const params = useSearchParams();
  const bookingId = params.get("booking_id");
  const { user } = useAuth();
  const router = useRouter();
  const [phase, setPhase] = useState<Phase>("starting");
  const [pay, setPay] = useState<InitiatePaymentResponse | null>(null);
  const [error, setError] = useState("");
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

  useEffect(() => {
    if (!bookingId || !user) return;

    // Step 1: initiate the payment session and redirect to the hosted
    // checkout page (Xendit renders the payment methods).
    api
      .post<InitiatePaymentResponse>(`/api/payments/booking/${bookingId}`)
      .then((res) => {
        if (res.payment_link_url) {
          setPay(res);
          window.location.href = res.payment_link_url;
          return;
        }
        setPhase("status");
      })
      .catch((err) => {
        const msg = err instanceof Error ? err.message : "";
        if (msg.includes("already initiated") || msg.includes("already processed")) {
          // A session already exists — resume by re-fetching its link.
          api
            .get<PaymentStatusResponse>(`/api/payments/booking/${bookingId}`)
            .then((s) => {
              if (s.payment_link_url) {
                window.location.href = s.payment_link_url;
                return;
              }
              setPhase("status");
            })
            .catch(() => setPhase("status"));
          return;
        }
        if (msg.includes("expired")) {
          setPhase("failed");
          setError("This booking has expired. Please make a new reservation.");
          return;
        }
        setError(msg || "Failed to start payment.");
      });
  }, [bookingId, user]);

  // Step 2: after the customer returns from the hosted page, poll the
  // booking status until it settles.
  useEffect(() => {
    if (!bookingId || !user || phase !== "status") return;
    const MAX_WAIT = 300_000;
    const start = Date.now();
    let cancelled = false;

    pollRef.current = setInterval(async () => {
      if (cancelled) return;
      if (Date.now() - start > MAX_WAIT) {
        stopPolling();
        setError("Payment is still being processed. Please check your bookings later.");
        return;
      }
      try {
        const s = await api.get<PaymentStatusResponse>(
          `/api/payments/booking/${bookingId}`,
        );
        if (cancelled) return;
        if (s.status === "completed") {
          stopPolling();
          setPhase("completed");
        } else if (s.status === "expired") {
          stopPolling();
          setPhase("failed");
          setError("Payment expired. Please make a new reservation.");
        }
      } catch {
        // keep polling
      }
    }, 3000);

    return () => {
      cancelled = true;
    };
  }, [bookingId, user, phase, stopPolling]);

  if (!bookingId) {
    return (
      <div className="card text-center py-12 max-w-md mx-auto">
        <AlertTriangle className="w-8 h-8 text-[#D4CEC4] mx-auto mb-3" />
        <p className="text-[#8B8580] text-sm">
          No booking found. Start from an event page.
        </p>
      </div>
    );
  }

  return (
    <div className="max-w-md mx-auto">
      <div className="text-center mb-8">
        <div className="inline-flex items-center justify-center w-12 h-12 rounded-xl bg-[#D9381E]/6 mb-4">
          <CreditCard className="w-6 h-6 text-[#D9381E]" />
        </div>
        <p className="font-[family-name:var(--font-display)] text-3xl text-[#1A1817] mb-2">
          Checkout
        </p>
        <p className="text-[#8B8580] text-sm font-mono">
          Booking: {bookingId.slice(0, 12)}&hellip;
        </p>
      </div>

      {pay && (
        <div className="card-stub space-y-3 mb-4">
          <div className="flex items-center justify-between pb-3 border-b border-dashed border-[#E8E3DC]">
            <span className="text-sm text-[#8B8580]">Amount</span>
            <span className="font-[family-name:var(--font-display)] text-xl text-[#1A1817]">
              {formatIDR(pay.amount_cents)}
            </span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-sm text-[#8B8580]">Status</span>
            <span className="badge badge-yellow">pending</span>
          </div>
        </div>
      )}

      {error && (
        <div className="card text-center border-[#FECACA] bg-[#FFF5F5] space-y-3">
          <p className="text-[#D9381E] text-sm">{error}</p>
          <button
            onClick={() => router.push("/")}
            className="btn-outline text-sm"
          >
            Back to Events
          </button>
        </div>
      )}

      {phase === "starting" && !error && (
        <div className="card text-center py-8">
          <Loader2 className="w-6 h-6 text-[#D9381E] animate-spin mx-auto" />
          <p className="text-sm text-[#4A4541] mt-3">
            Starting payment&hellip;
          </p>
        </div>
      )}

      {phase === "status" && !error && (
        <div className="card text-center py-8">
          <Loader2 className="w-6 h-6 text-[#D9381E] animate-spin mx-auto" />
          <p className="text-sm text-[#4A4541] mt-3">
            Waiting for payment&hellip;
          </p>
          <p className="text-xs text-[#8B8580] mt-2">
            Complete the payment on the checkout page, then return here.
          </p>
        </div>
      )}

      {phase === "completed" && (
        <div className="space-y-3">
          <div className="card flex items-center gap-3 text-sm text-[#2D7A46] border-[#2D7A46]/20">
            <CreditCard className="w-4 h-4" />
            <span>Payment successful</span>
          </div>
          <button
            onClick={() => router.push(`/confirmation/${bookingId}`)}
            className="btn-accent w-full"
          >
            View Confirmation <ArrowRight className="w-4 h-4" />
          </button>
        </div>
      )}

      {phase === "failed" && (
        <div className="space-y-3">
          <div className="card flex items-center gap-3 text-sm text-[#D9381E] border-[#FECACA]">
            <Ticket className="w-4 h-4" />
            <span>
              {error || "Payment expired. Your reservation was released."}
            </span>
          </div>
          <div className="flex gap-3">
            <button
              onClick={() => router.push("/")}
              className="btn-outline flex-1"
            >
              Back to Events
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

export default function CheckoutPage() {
  return (
    <Suspense
      fallback={
        <div className="max-w-md mx-auto card text-center py-10">
          <Loader2 className="w-6 h-6 text-[#D9381E] animate-spin mx-auto" />
        </div>
      }
    >
      <CheckoutForm />
    </Suspense>
  );
}

"use client";

import { useQuery, useMutation } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { api } from "@/lib/api-client";
import { fmtDateTime, fmtIDR } from "@/lib/format";
import { toast } from "@/components/ui/toast";
import { AlertTriangle, ArrowRight, Loader2, ShoppingBag, Ticket } from "lucide-react";
import type { BookingResponse, InitiatePaymentResponse, PaymentStatusResponse } from "@/types";

export default function CheckoutPage() {
  const { booking_id } = useParams<{ booking_id: string }>();
  const router = useRouter();

  const bookingQuery = useQuery({
    queryKey: ["booking", booking_id],
    queryFn: () => api.get<BookingResponse>(`/api/bookings/${booking_id}`),
    staleTime: 10 * 1000,
  });

  const booking = bookingQuery.data;

  const initiateMutation = useMutation({
    mutationFn: (bookingId: string) =>
      api.post<InitiatePaymentResponse>(`/api/payments/booking/${bookingId}`),
    onSuccess: (data) => {
      router.push("/payment/" + data.transaction_id);
    },
    onError: (err: Error) => {
      const msg = err.message;
      if (msg.includes("already initiated")) {
        // A session already exists — resume it on the payment page.
        api
          .get<PaymentStatusResponse>(`/api/payments/booking/${booking_id}`)
          .then((s) => {
            if (s.transaction_id) {
              router.push("/payment/" + s.transaction_id);
              return;
            }
            toast.error("Failed to resume payment.");
          })
          .catch(() => toast.error("Failed to resume payment."));
        return;
      }
      if (msg.includes("expired")) {
        toast.error("This booking has expired. Please make a new reservation.");
        return;
      }
      toast.error(msg || "Failed to start payment.");
    },
  });

  if (bookingQuery.isLoading) {
    return (
      <div className="max-w-md mx-auto card text-center py-12">
        <Loader2 className="w-6 h-6 text-[#D9381E] animate-spin mx-auto" />
        <p className="text-sm text-[#4A4541] mt-3">Loading booking&hellip;</p>
      </div>
    );
  }

  if (bookingQuery.isError || !booking) {
    return (
      <div className="max-w-md mx-auto card text-center py-12 space-y-4">
        <Ticket className="w-8 h-8 text-[#D4CEC4] mx-auto" />
        <p className="text-[#8B8580] text-sm">Booking not found</p>
        <button onClick={() => router.push("/")} className="btn-outline text-sm">
          Back to Events
        </button>
      </div>
    );
  }

  const badgeClass =
    booking.status === "confirmed" ? "badge-green" :
    booking.status === "pending" ? "badge-yellow" : "badge-red";

  const totalItems = booking.items.reduce((sum, i) => sum + i.quantity, 0);

  return (
    <div className="max-w-md mx-auto space-y-4">
      <div className="text-center mb-6">
        <div className="inline-flex items-center justify-center w-12 h-12 rounded-xl bg-[#D9381E]/6 mb-4">
          <ShoppingBag className="w-6 h-6 text-[#D9381E]" />
        </div>
        <p className="font-[family-name:var(--font-display)] text-3xl text-[#1A1817] mb-2">Checkout</p>
        <p className="text-[#8B8580] text-sm font-mono">
          Booking: {booking.id.slice(0, 12)}&hellip;
        </p>
      </div>

      <div className="card space-y-3">
        <div className="flex items-center justify-between">
          <span className="text-sm text-[#8B8580]">Status</span>
          <span className={`badge ${badgeClass}`}>{booking.status}</span>
        </div>

        <div className="border-t border-dashed border-[#E8E3DC] pt-3 space-y-2">
          {booking.items.map((item) => (
            <div key={item.id} className="flex justify-between text-sm">
              <span className="text-[#4A4541]">
                {item.quantity} &times; Ticket
              </span>
              <span className="font-medium text-[#1A1817]">
                {fmtIDR(item.unit_price_rupiah * item.quantity)}
              </span>
            </div>
          ))}
        </div>

        <div className="border-t border-dashed border-[#E8E3DC] pt-3 flex justify-between items-center">
          <span className="font-semibold text-[#1A1817]">
            Total ({totalItems} ticket{totalItems !== 1 ? "s" : ""})
          </span>
          <span className="font-[family-name:var(--font-display)] text-xl text-[#1A1817]">
            {fmtIDR(booking.total_rupiah)}
          </span>
        </div>

        {booking.expires_at && booking.status === "pending" && (
          <p className="text-xs text-[#8B8580] border-t border-dashed border-[#E8E3DC] pt-3">
            Reservation expires at {fmtDateTime(booking.expires_at)}.
          </p>
        )}
      </div>

      {booking.status === "pending" && (
        <button
          onClick={() => initiateMutation.mutate(booking.id)}
          disabled={initiateMutation.isPending}
          className="btn-accent w-full py-3"
        >
          {initiateMutation.isPending ? (
            <Loader2 className="w-4 h-4 animate-spin mx-auto" />
          ) : (
            <span className="flex items-center justify-center gap-2">
              Pay Now <ArrowRight className="w-4 h-4" />
            </span>
          )}
        </button>
      )}

      {booking.status === "confirmed" && (
        <div className="space-y-3">
          <div className="card flex items-center gap-3 text-sm text-[#2D7A46] border-[#2D7A46]/20">
            <Ticket className="w-4 h-4" />
            <span>Payment completed — tickets confirmed</span>
          </div>
          <Link
            href={`/confirmation/${booking.id}`}
            className="btn-accent w-full text-center flex items-center justify-center gap-1.5"
          >
            View Confirmation <ArrowRight className="w-4 h-4" />
          </Link>
        </div>
      )}

      {(booking.status === "expired" || booking.status === "cancelled") && (
        <div className="card flex items-center gap-3 text-sm text-[#D9381E] border-[#FECACA]">
          <AlertTriangle className="w-4 h-4" />
          <span>
            {booking.status === "expired"
              ? "This reservation has expired. Please make a new reservation."
              : "This booking was cancelled."}
          </span>
        </div>
      )}
    </div>
  );
}

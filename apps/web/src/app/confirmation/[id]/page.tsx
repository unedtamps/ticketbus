"use client";

import { useQuery } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { api } from "@/lib/api-client";
import { CheckCircle2, LayoutDashboard, Ticket } from "lucide-react";

interface BookingItemResp {
  id: string;
  ticket_type_id: string;
  quantity: number;
  unit_price_cents: number;
}

interface BookingResponse {
  id: string;
  event_id: string;
  status: string;
  total_cents: number;
  payment_id: string;
  refund_status?: string;
  items: BookingItemResp[];
  created_at: string;
}

export default function ConfirmationPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();

  const bookingQuery = useQuery({
    queryKey: ["booking", id],
    queryFn: () => api.get<BookingResponse>(`/api/bookings/${id}`),
    staleTime: 60 * 1000,
  });

  const booking = bookingQuery.data;

  if (bookingQuery.isLoading) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <div className="card-stub text-center max-w-md mx-auto py-12 px-8 space-y-4">
          <div className="skeleton h-16 w-16 rounded-xl mx-auto" />
          <div className="skeleton h-8 w-48 mx-auto" />
          <div className="skeleton h-24 w-full" />
        </div>
      </div>
    );
  }

  if (bookingQuery.isError || !booking) {
    return (
      <div className="flex items-center justify-center min-h-[60vh] fade-up">
        <div className="card-stub text-center max-w-md mx-auto py-12 px-8 space-y-5">
          <div className="w-14 h-14 rounded-xl bg-[#D4CEC4]/20 flex items-center justify-center mx-auto">
            <Ticket className="w-7 h-7 text-[#B0A89E]" />
          </div>
          <p className="font-[family-name:var(--font-display)] text-2xl text-[#1A1817]">
            Booking Not Found
          </p>
          <p className="text-[#8B8580] text-sm">
            We couldn&apos;t find this booking. It may have been removed or the link is incorrect.
          </p>
          <button onClick={() => router.push("/dashboard")} className="btn-accent">
            View Dashboard
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex items-center justify-center min-h-[60vh] fade-up">
      <div className="card-stub max-w-md mx-auto py-10 px-8 space-y-5">
        {/* Success checkmark */}
        <div className="w-16 h-16 rounded-full bg-[#2D7A46]/10 flex items-center justify-center mx-auto">
          <CheckCircle2 className="w-9 h-9 text-[#2D7A46]" />
        </div>

        {/* Heading */}
        <div className="text-center space-y-1">
          <p className="font-[family-name:var(--font-display)] text-2xl text-[#1A1817]">
            Booking Confirmed
          </p>
          <div className="flex items-center justify-center gap-2">
            <span className="text-xs text-[#8B8580] font-mono">
              {booking.id.slice(0, 12)}&hellip;
            </span>
            <span className={`badge ${
              booking.status === "confirmed" ? "badge-green" :
              booking.status === "cancelled" ? "badge-red" : "badge-yellow"
            }`}>
              {booking.status}
            </span>
          </div>
        </div>

        {/* Dashed divider */}
        <div className="border-t border-dashed border-[#E8E3DC]" />

        {/* Ticket items */}
        {booking.items.length > 0 && (
          <div className="space-y-2">
            <p className="text-xs font-medium text-[#B0A89E] uppercase tracking-wider">
              Tickets
            </p>
            {booking.items.map((item) => (
              <div key={item.id} className="flex justify-between text-sm">
                <span className="text-[#4A4541]">
                  <span className="text-[#8B8580]">{item.quantity} &times;</span> Ticket
                </span>
                <span className="font-medium text-[#1A1817]">
                  ${((item.unit_price_cents * item.quantity) / 100).toFixed(2)}
                </span>
              </div>
            ))}
          </div>
        )}

        {/* Total */}
        <div className="border-t border-dashed border-[#E8E3DC] pt-3 flex justify-between items-center">
          <span className="text-sm font-semibold text-[#1A1817]">Total</span>
          <span className="font-[family-name:var(--font-display)] text-xl text-[#1A1817]">
            ${(booking.total_cents / 100).toFixed(2)}
          </span>
        </div>

        {/* Actions */}
        <div className="flex gap-3 pt-2">
          <Link href="/" className="btn-ghost flex-1 text-center text-sm">
            Back to Events
          </Link>
          <Link href="/dashboard" className="btn-accent flex-1 text-center text-sm flex items-center justify-center gap-1.5">
            <LayoutDashboard className="w-4 h-4" />
            View Dashboard
          </Link>
        </div>
      </div>
    </div>
  );
}

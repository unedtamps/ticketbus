"use client";

import { useQuery } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { fmtIDR } from "@/lib/format";
import { api } from "@/lib/api-client";
import { CheckCircle2, LayoutDashboard, Ticket } from "lucide-react";

interface BookingItemResp {
  id: string;
  ticket_type_id: string;
  quantity: number;
  unit_price_rupiah: number;
}

interface BookingResponse {
  id: string;
  event_id: string;
  status: string;
  total_rupiah: number;
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
        <div className="card text-center max-w-md mx-auto py-12 px-8 space-y-4 w-full">
          <div className="skeleton h-16 w-16 rounded-full mx-auto" />
          <div className="skeleton h-8 w-48 mx-auto" />
          <div className="skeleton h-24 w-full" />
        </div>
      </div>
    );
  }

  if (bookingQuery.isError || !booking) {
    return (
      <div className="flex items-center justify-center min-h-[60vh] fade-up">
        <div className="card text-center max-w-md mx-auto py-12 px-8 space-y-5">
          <div className="w-14 h-14 rounded-full bg-[#EDF1F7] flex items-center justify-center mx-auto">
            <Ticket className="w-7 h-7 text-[#9AA6BC]" />
          </div>
          <p className="font-[family-name:var(--font-display)] text-2xl text-[#16224A]">
            Booking Not Found
          </p>
          <p className="text-[#6E7B96] text-sm">
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
      <div className="card max-w-md mx-auto py-10 px-8 space-y-5 w-full">
        {/* Success checkmark */}
        <div className="w-16 h-16 rounded-full bg-[#E7EEFF] flex items-center justify-center mx-auto">
          <CheckCircle2 className="w-9 h-9 text-[#2450D9]" />
        </div>

        {/* Heading */}
        <div className="text-center space-y-1.5">
          <p className="font-[family-name:var(--font-display)] text-[28px] text-[#16224A]">
            Booking Confirmed
          </p>
          <div className="flex items-center justify-center gap-2">
            <span className="badge badge-blue">
              BOOKING {booking.id.slice(0, 12).toUpperCase()}
            </span>
            <span className={`badge ${
              booking.status === "confirmed" ? "badge-green" :
              booking.status === "cancelled" ? "badge-ink" : "badge-yellow"
            }`}>
              {booking.status === "confirmed" ? "LUNAS" : booking.status}
            </span>
          </div>
        </div>

        <div className="border-t border-[#E4EAF4]" />

        {/* Ticket items */}
        {booking.items.length > 0 && (
          <div className="space-y-2">
            <p className="text-xs font-extrabold text-[#6E7B96] uppercase tracking-wider">
              Tickets
            </p>
            {booking.items.map((item) => (
              <div key={item.id} className="flex justify-between text-sm">
                <span className="text-[#3D4A66] font-semibold">
                  <span className="text-[#6E7B96]">{item.quantity} &times;</span> Ticket
                </span>
                <span className="font-bold text-[#16224A]">
                  {fmtIDR(item.unit_price_rupiah * item.quantity)}
                </span>
              </div>
            ))}
          </div>
        )}

        {/* Total */}
        <div className="border-t border-[#E4EAF4] pt-3 flex justify-between items-center">
          <span className="text-sm font-bold text-[#6E7B96]">Total</span>
          <span className="font-[family-name:var(--font-display)] text-2xl text-[#16224A]">
            {fmtIDR(booking.total_rupiah)}
          </span>
        </div>

        {/* Actions */}
        <div className="flex gap-3 pt-2">
          <Link href="/" className="btn-outline flex-1 text-center text-sm">
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

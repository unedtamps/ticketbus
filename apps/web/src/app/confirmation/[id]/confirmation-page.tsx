"use client";

import { useQuery } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { fmtIDR } from "@/lib/format";
import { api } from "@/lib/api-client";
import { useAuth } from "@/lib/auth-context";
import { CheckCircle2, LayoutDashboard, Ticket } from "lucide-react";
import type { BookingResponse } from "@/types";

/** Stub header monogram, e.g. "PSI" for "Pasar Seni Indie Bandung". */
function initials(title: string) {
  const words = title.split(/\s+/).filter(Boolean).slice(0, 3);
  if (words.length === 0) return "TKT";
  return words.map((w) => w[0]!.toUpperCase()).join("");
}

export default function ConfirmationPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const { user } = useAuth();

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
            Pesanan tidak ditemukan
          </p>
          <p className="text-[#6E7B96] text-sm">
            Pesanan ini mungkin sudah dihapus, atau tautannya salah.
          </p>
          <button onClick={() => router.push("/dashboard")} className="btn-accent">
            Buka Dasbor
          </button>
        </div>
      </div>
    );
  }

  const totalQty = booking.items.reduce((n, i) => n + i.quantity, 0);
  const eventTitle = booking.event_title || "Tiket";
  const monogram = initials(eventTitle);

  return (
    <div className="max-w-4xl mx-auto fade-up">
      {/* Outcome — driven by the booking's real status, never assumed. */}
      <div className="text-center mt-4 mb-9">
        <div className="w-16 h-16 rounded-full bg-[#E7EEFF] flex items-center justify-center mx-auto mb-5">
          <CheckCircle2 className="w-8 h-8 text-[#2450D9]" />
        </div>
        <p className="font-[family-name:var(--font-display)] text-[38px] leading-tight text-[#16224A]">
          {booking.status === "confirmed"
            ? "Pembayaran diterima!"
            : booking.status === "pending"
              ? "Menunggu pembayaran"
              : "Pesanan dibatalkan"}
        </p>
        <p className="text-[#6E7B96] text-[14.5px] mt-3 max-w-lg mx-auto leading-relaxed">
          {booking.status === "confirmed" ? (
            <>
              {totalQty} tiket {booking.items.map((i) => i.ticket_type_name || "Tiket").join(", ")}{" "}
              untuk {eventTitle} sudah terbit dan dikirim ke {user?.email || "email Anda"}.
            </>
          ) : booking.status === "pending" ? (
            <>Pesanan ini belum lunas — tiket terbit setelah pembayaran dikonfirmasi.</>
          ) : (
            <>Pesanan ini sudah tidak aktif.</>
          )}
        </p>
        <div className="flex items-center justify-center gap-2 mt-4">
          <span className="badge badge-blue">
            BOOKING {booking.id.slice(0, 8).toUpperCase()}
          </span>
          <span
            className={`badge ${
              booking.status === "confirmed"
                ? "badge-green"
                : booking.status === "cancelled"
                  ? "badge-ink"
                  : "badge-yellow"
            }`}
          >
            {booking.status === "confirmed" ? "LUNAS" : booking.status.toUpperCase()}
          </span>
        </div>
      </div>

      {/* One stub per ticket line, regardless of status — the same layout
          whether or not the payment has settled, so the page does not change
          shape under the customer. The backend has no per-attendee ticket
          entity, so each stub represents the ticket type and quantity bought:
          it does not invent holder names or seat numbers. */}
      {booking.items.length > 0 && (
        <div className="grid gap-5 sm:grid-cols-2 mb-8">
          {booking.items.map((item) => (
            <div
              key={item.id}
              className="rounded-2xl border border-[#E4EAF4] overflow-hidden bg-white"
            >
              <div className="flex items-center justify-between bg-[#FFD43B] px-5 py-3.5">
                <span className="font-[family-name:var(--font-display)] text-xl font-bold text-[#16224A]">
                  {monogram}
                </span>
                <span className="text-[10px] font-extrabold uppercase tracking-[0.14em] text-[#16224A]">
                  E-Tiket
                </span>
              </div>
              <div className="px-5 py-4">
                <p className="text-[10px] font-extrabold uppercase tracking-[0.1em] text-[#9AA6BC]">
                  {eventTitle}
                </p>
                <p className="text-[15px] font-extrabold text-[#16224A] mt-1.5">
                  {item.ticket_type_name || "Tiket"}
                </p>
                <div className="flex items-end justify-between gap-3 mt-3">
                  <div className="space-y-1">
                    <p className="text-[12.5px] text-[#6E7B96]">
                      {item.quantity} tiket &middot; {fmtIDR(item.unit_price_rupiah)} / tiket
                    </p>
                    {booking.event_venue && (
                      <p className="text-[12.5px] text-[#6E7B96]">{booking.event_venue}</p>
                    )}
                  </div>
                  <div className="flex-shrink-0 w-11 h-11 rounded-lg bg-[#F6F9FC] border border-[#E4EAF4] grid place-items-center">
                    <Ticket className="w-5 h-5 text-[#9AA6BC]" />
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Totals */}
      <div className="card space-y-2 mb-7">
        {booking.items.map((item) => (
          <div key={item.id} className="flex justify-between text-sm">
            <span className="text-[#3D4A66]">
              {item.quantity} &times; {item.ticket_type_name || "Tiket"}
            </span>
            <span className="font-semibold text-[#16224A]">
              {fmtIDR(item.unit_price_rupiah * item.quantity)}
            </span>
          </div>
        ))}
        <div className="border-t border-[#E4EAF4] pt-2.5 flex justify-between items-center">
          <span className="text-sm font-bold text-[#6E7B96]">Total</span>
          <span className="font-[family-name:var(--font-display)] text-2xl text-[#16224A]">
            {fmtIDR(booking.total_rupiah)}
          </span>
        </div>
      </div>

      {/* Actions */}
      <div className="flex flex-wrap justify-center gap-3 pb-8">
        <Link href="/dashboard" className="btn-accent text-sm flex items-center justify-center gap-1.5 px-6">
          <LayoutDashboard className="w-4 h-4" />
          Buka Dasbor
        </Link>
        <Link href="/" className="btn-outline text-sm text-center px-6">
          Kembali ke beranda
        </Link>
      </div>
    </div>
  );
}

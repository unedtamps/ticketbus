"use client";

import { useEffect, useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { api } from "@/lib/api-client";
import { useAuth } from "@/lib/auth-context";
import { fmtIDR } from "@/lib/format";
import { LocalTime } from "@/components/ui/local-time";
import { toast } from "@/components/ui/toast";
import {
  AlertTriangle,
  ArrowRight,
  Info,
  Loader2,
  RefreshCw,
  ShoppingBag,
  Ticket,
  Timer,
} from "lucide-react";
import type { BookingResponse, EventDetail, InitiatePaymentResponse, PaymentStatusResponse } from "@/types";

// Live 1s clock for the hold countdown. Starts null so the server render and
// the first client frame agree; the interval only runs on the client.
function useNowMs() {
  const [nowMs, setNowMs] = useState<number | null>(null);
  useEffect(() => {
    setNowMs(Date.now());
    const t = setInterval(() => setNowMs(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  return nowMs;
}

function fmtCountdown(ms: number) {
  const total = Math.max(0, Math.floor(ms / 1000));
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}

export default function CheckoutPage() {
  const { booking_id } = useParams<{ booking_id: string }>();
  const router = useRouter();
  const { user } = useAuth();
  // The payment transaction is created asynchronously (outbox → background
  // dispatch to the payment service), so a very fast "Pay Now" can race the
  // dispatcher and get a 404. We surface a wait/refresh state instead of an
  // error; the row typically lands within a second.
  const [txnNotReady, setTxnNotReady] = useState(false);
  const nowMs = useNowMs();

  const bookingQuery = useQuery({
    queryKey: ["booking", booking_id],
    queryFn: () => api.get<BookingResponse>(`/api/bookings/${booking_id}`),
    staleTime: 10 * 1000,
  });

  const booking = bookingQuery.data;

  // The summary names ticket types and shows the event line; the booking
  // items only carry ticket_type_id, so the event detail fills in names.
  const eventQuery = useQuery({
    queryKey: ["event", booking?.event_id],
    queryFn: () => api.get<EventDetail>(`/api/events/${booking!.event_id}`),
    enabled: !!booking,
  });

  const initiateMutation = useMutation({
    mutationFn: (bookingId: string) =>
      api.post<InitiatePaymentResponse>(`/api/payments/booking/${bookingId}`),
    onSuccess: (data) => {
      setTxnNotReady(false);
      router.push("/payment/" + data.transaction_id);
    },
    onError: (err: Error) => {
      const msg = err.message;
      const status = (err as Error & { status?: number }).status;
      if (status === 404 || msg.includes("transaction not found")) {
        // Not created yet by the background dispatcher — not a real failure.
        setTxnNotReady(true);
        return;
      }
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
        <Loader2 className="w-6 h-6 text-[#2450D9] animate-spin mx-auto" />
        <p className="text-sm text-[#6E7B96] mt-3">Loading booking&hellip;</p>
      </div>
    );
  }

  if (bookingQuery.isError || !booking) {
    return (
      <div className="max-w-md mx-auto card text-center py-12 space-y-4">
        <Ticket className="w-8 h-8 text-[#C9D4E8] mx-auto" />
        <p className="text-[#6E7B96] text-sm">Booking not found</p>
        <button onClick={() => router.push("/")} className="btn-outline text-sm">
          Back to Events
        </button>
      </div>
    );
  }

  const event = eventQuery.data?.event;
  const typeNames = new Map(
    (eventQuery.data?.ticket_types || []).map((tt) => [tt.id, tt.name]),
  );

  const holdMsLeft =
    booking.expires_at && nowMs !== null
      ? new Date(booking.expires_at).getTime() - nowMs
      : null;
  const holdExpired = holdMsLeft !== null && holdMsLeft <= 0;

  const totalItems = booking.items.reduce((sum, i) => sum + i.quantity, 0);

  const titleInitials = (event?.title || "E")
    .split(/\s+/)
    .slice(0, 2)
    .map((w) => w[0]!.toUpperCase())
    .join("");

  return (
    <div className="max-w-5xl mx-auto">
      {/* Title row */}
      <div className="flex flex-wrap items-end justify-between gap-2 mb-6">
        <p className="font-[family-name:var(--font-display)] text-3xl text-[#16224A]">Checkout</p>
        <p className="text-[13px] font-semibold text-[#6E7B96]">
          Langkah 2 dari 3 &middot; Konfirmasi pesanan
        </p>
      </div>

      {/* Hold banner with live countdown */}
      {booking.status === "pending" && booking.expires_at && (
        <div className="flex items-center justify-between gap-3 rounded-xl bg-[#FFF4CC] px-4 py-3 mb-6">
          <p className="flex items-center gap-2.5 text-[13px] font-bold text-[#8A6400]">
            <Timer className="w-4 h-4 flex-shrink-0" />
            {holdExpired
              ? "Reservasi kedaluwarsa — silakan buat pesanan baru."
              : holdMsLeft === null
                ? "Reservasi ditahan — kursi dikunci sampai batas waktu."
                : `Reservasi ditahan — kursi dikunci sampai ${fmtCountdown(holdMsLeft)} lagi.`}
          </p>
          <p className="text-xs font-bold text-[#8A6400] tracking-wide">
            TB-{booking.id.slice(0, 4).toUpperCase()}
          </p>
        </div>
      )}

      {/* Main: details left, summary right */}
      <div className="flex flex-col lg:flex-row gap-7 items-start">
        <div className="flex-1 min-w-0 w-full space-y-6">
          {/* Detail pesanan — read-only; the reservation belongs to the
              logged-in account, and the backend takes no separate payer data. */}
          <section className="space-y-3">
            <p className="text-[11px] font-extrabold uppercase tracking-[1.4px] text-[#6E7B96]">
              Detail Pesanan
            </p>
            <div className="card space-y-4">
              <div className="grid sm:grid-cols-2 gap-4">
                <div>
                  <p className="text-[13px] font-bold text-[#16224A] mb-1.5">Nama lengkap</p>
                  <div className="input-field">{user?.name || "—"}</div>
                </div>
                <div>
                  <p className="text-[13px] font-bold text-[#16224A] mb-1.5">Email</p>
                  <div className="input-field truncate">{user?.email || "—"}</div>
                </div>
              </div>
              <p className="text-xs text-[#6E7B96]">
                Tiket dan e-tiket dikirim ke akun ini — ubahnya lewat profil jika perlu.
              </p>
            </div>
          </section>

          {/* Xendit explainer */}
          <div className="flex items-center gap-2.5 rounded-xl bg-[#F0F5FF] px-4 py-3">
            <Info className="w-4 h-4 text-[#2450D9] flex-shrink-0" />
            <p className="text-[13px] font-semibold leading-relaxed text-[#16224A]">
              Pembayaran via Xendit — setelah ini halaman pembayaran terbuka di
              tab baru untuk memilih QRIS, Virtual Account, atau E-Wallet.
            </p>
          </div>

          {booking.status === "confirmed" && (
            <div className="card flex items-center gap-3 text-sm font-semibold text-[#2450D9] bg-[#E7EEFF] border-[#E7EEFF]">
              <Ticket className="w-4 h-4" />
              <span>Payment completed — tickets confirmed</span>
            </div>
          )}

          {(booking.status === "expired" || booking.status === "cancelled") && (
            <div className="card flex items-center gap-3 text-sm font-semibold text-[#8A6400] bg-[#FFF4CC] border-[#FFF4CC]">
              <AlertTriangle className="w-4 h-4" />
              <span>
                {booking.status === "expired"
                  ? "This reservation has expired. Please make a new reservation."
                  : "This booking was cancelled."}
              </span>
            </div>
          )}
        </div>

        {/* Ringkasan */}
        <aside className="card w-full lg:w-[400px] flex-shrink-0 space-y-3.5">
          <p className="font-[family-name:var(--font-display)] text-xl text-[#16224A]">Ringkasan</p>

          <div className="flex items-center gap-3">
            {event?.image_url ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img
                src={event.image_url}
                alt=""
                className="w-16 h-16 rounded-xl object-cover flex-shrink-0"
              />
            ) : (
              <div className="w-16 h-16 rounded-xl bg-[#E7EEFF] text-[#2450D9] flex items-center justify-center font-[family-name:var(--font-display)] text-lg flex-shrink-0">
                {titleInitials}
              </div>
            )}
            <div className="min-w-0">
              <p className="text-sm font-bold text-[#16224A] truncate">
                {event ? (
                  <Link href={`/events/${event.id}`} className="hover:text-[#2450D9] transition-colors">
                    {event.title}
                  </Link>
                ) : (
                  "…"
                )}
              </p>
              {event && (
                <p className="text-xs font-medium text-[#6E7B96] mt-0.5 truncate">
                  <LocalTime iso={event.start_at} format="short" /> &middot; {event.venue_name}
                </p>
              )}
            </div>
          </div>

          <div className="h-px bg-[#E4EAF4]" />

          <div className="space-y-2">
            {booking.items.map((item) => (
              <div key={item.id} className="flex justify-between gap-3 text-[13px]">
                <span className="font-medium text-[#6E7B96]">
                  {item.quantity} &times; {typeNames.get(item.ticket_type_id) || "Ticket"}
                </span>
                <span className="font-semibold text-[#16224A] whitespace-nowrap">
                  {fmtIDR(item.unit_price_rupiah * item.quantity)}
                </span>
              </div>
            ))}
          </div>

          <div className="h-px bg-[#E4EAF4]" />

          <div className="flex justify-between items-end">
            <span className="text-sm font-bold text-[#6E7B96]">
              Total ({totalItems} ticket{totalItems !== 1 ? "s" : ""})
            </span>
            <span className="font-[family-name:var(--font-display)] text-[26px] leading-none text-[#16224A]">
              {fmtIDR(booking.total_rupiah)}
            </span>
          </div>

          {booking.status === "pending" && !txnNotReady && (
            <button
              onClick={() => initiateMutation.mutate(booking.id)}
              disabled={initiateMutation.isPending || holdExpired}
              className="btn-accent w-full py-3"
            >
              {initiateMutation.isPending ? (
                <Loader2 className="w-4 h-4 animate-spin mx-auto" />
              ) : (
                <span className="flex items-center justify-center gap-2">
                  Bayar Sekarang <ArrowRight className="w-4 h-4" />
                </span>
              )}
            </button>
          )}

          {booking.status === "pending" && txnNotReady && (
            <button
              onClick={() => initiateMutation.mutate(booking.id)}
              disabled={initiateMutation.isPending}
              className="btn-outline w-full py-3 flex items-center justify-center gap-2"
            >
              {initiateMutation.isPending ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <>
                  <RefreshCw className="w-4 h-4" /> Payment setup is being prepared — refresh
                </>
              )}
            </button>
          )}

          {booking.status === "confirmed" && (
            <Link
              href={`/confirmation/${booking.id}`}
              className="btn-accent w-full text-center flex items-center justify-center gap-1.5"
            >
              Lihat E-Tiket <ArrowRight className="w-4 h-4" />
            </Link>
          )}

          <p className="text-xs font-medium leading-relaxed text-[#6E7B96]">
            Kursi tetap ditahan selama proses pembayaran. Halaman pembayaran
            memakai branding Xendit — logo &amp; nama bisnis dari dashboard.
          </p>
        </aside>
      </div>
    </div>
  );
}

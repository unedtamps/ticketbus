"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { api } from "@/lib/api-client";
import { fmtIDR } from "@/lib/format";
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  CreditCard,
  ExternalLink,
  Loader2,
  Lock,
  RefreshCw,
  Timer,
} from "lucide-react";
import type { BookingResponse, TransactionResponse } from "@/types";

/** Ticks once a second on the client only, so the server-rendered first frame
 *  and the first client frame agree. */
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
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const mmss = `${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
  return h > 0 ? `${h}:${mmss}` : mmss;
}

export default function PaymentPage() {
  const { txn_id } = useParams<{ txn_id: string }>();
  const router = useRouter();
  const nowMs = useNowMs();

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

  // The order summary comes from ticketing: the payment service only knows
  // amounts, so the event title and ticket names have to be read from the
  // booking the transaction belongs to.
  const bookingQuery = useQuery({
    queryKey: ["booking", txn?.booking_id],
    queryFn: () => api.get<BookingResponse>(`/api/bookings/${txn!.booking_id}`),
    enabled: !!txn?.booking_id,
    staleTime: 60 * 1000,
  });
  const booking = bookingQuery.data;

  // The hosted checkout is never opened automatically. Popup blockers drop
  // window.open calls that do not come from a click, and a tab opening on its
  // own is disorienting — the customer leaves through the button below.

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

  const isOpen = txn.status === "pending" || txn.status === "initiated";

  // A session is live, so the hold is what governs how long the customer has
  // left to pay — and expires_at is that same deadline, since the session was
  // created with it. This is the page where the payment actually gets
  // finished, so the countdown belongs here.
  const payMsLeft =
    nowMs === null || !txn.expires_at
      ? null
      : new Date(txn.expires_at).getTime() - nowMs;
  const payExpired = payMsLeft !== null && payMsLeft <= 0;

  const totalQty = booking?.items.reduce((n, i) => n + i.quantity, 0) ?? 0;

  return (
    <div className="max-w-5xl mx-auto">
      {/* Header row with the live countdown. */}
      <div className="flex flex-wrap items-start justify-between gap-4 mb-7">
        <div>
          <p className="font-[family-name:var(--font-display)] text-[32px] leading-tight text-[#16224A]">
            {isOpen ? "Menunggu pembayaran" : txn.status === "completed" ? "Pembayaran diterima" : "Pembayaran kedaluwarsa"}
          </p>
          <p className="text-[#6E7B96] text-sm mt-1.5">
            {isOpen
              ? "Selesaikan pembayaran di tab Xendit — tab ini yang memantau sampai selesai."
              : txn.status === "completed"
                ? "Pembayaran sudah dikonfirmasi. E-tiket dikirim ke akun Anda."
                : "Sesi pembayaran sudah ditutup dan kursi dilepas."}
          </p>
        </div>
        {isOpen && payMsLeft !== null && !payExpired && (
          <span className="inline-flex items-center gap-2 rounded-full bg-[#FFD43B] px-4 py-2.5 text-[13px] font-extrabold text-[#16224A] whitespace-nowrap">
            <Timer className="w-4 h-4" />
            KEDALUWARSA {fmtCountdown(payMsLeft)}
          </span>
        )}
        {isOpen && payExpired && (
          <span className="inline-flex items-center gap-2 rounded-full bg-[#EDF1F7] px-4 py-2.5 text-[13px] font-extrabold text-[#5F6B7E] whitespace-nowrap">
            <Timer className="w-4 h-4" />
            WAKTU HABIS
          </span>
        )}
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,380px)] items-start">
        {/* Left: the payment action. */}
        <div className="space-y-4">
          <div className="card space-y-4">
            <div className="flex items-start justify-between gap-3">
              <div className="flex items-start gap-3">
                <div className="icon-tile flex-shrink-0">
                  <ExternalLink className="w-[18px] h-[18px]" />
                </div>
                <div>
                  <p className="text-[15px] font-extrabold text-[#16224A]">
                    Halaman Pembayaran Xendit
                  </p>
                  <p className="text-xs text-[#9AA6BC] mt-0.5">
                    payment link &middot; tab baru &middot; branding Xendit
                  </p>
                </div>
              </div>
              <span
                className={`badge ${
                  txn.status === "completed"
                    ? "badge-green"
                    : txn.status === "expired"
                      ? "badge-ink"
                      : "badge-yellow"
                }`}
              >
                {txn.status}
              </span>
            </div>

            {txn.payment_link_url && isOpen && (
              <a
                href={txn.payment_link_url}
                target="_blank"
                rel="noopener noreferrer"
                className="btn-accent w-full py-3 text-sm flex items-center justify-center gap-2"
              >
                Buka Halaman Pembayaran
                <ExternalLink className="w-4 h-4" />
              </a>
            )}

            {isOpen && (
              <div className="flex items-start gap-2.5 rounded-xl bg-[#FFF4CC] px-3.5 py-3">
                <Timer className="w-4 h-4 flex-shrink-0 text-[#8A6400] mt-0.5" />
                <p className="text-[12.5px] font-semibold text-[#8A6400] leading-relaxed">
                  {payExpired
                    ? "Waktu pembayaran habis — kursi akan dilepas. Silakan buat pesanan baru."
                    : "Kursi masih ditahan — selesaikan pembayaran sebelum waktu habis."}
                </p>
              </div>
            )}

            {isOpen && (
              <div className="flex items-start gap-2.5 rounded-xl bg-[#F6F9FC] px-3.5 py-3">
                <RefreshCw className="w-4 h-4 flex-shrink-0 text-[#6E7B96] mt-0.5" />
                <p className="text-[12.5px] text-[#6E7B96] leading-relaxed">
                  Tab ini memantau otomatis — begitu pembayaran diterima, halaman
                  berubah jadi konfirmasi tanpa kamu refresh.
                </p>
              </div>
            )}
          </div>

          {txn.status === "completed" && (
            <div className="card space-y-3">
              <div className="flex items-center gap-2.5 text-sm font-bold text-[#2450D9]">
                <CheckCircle2 className="w-[18px] h-[18px]" />
                Pembayaran berhasil
              </div>
              <Link
                href={`/confirmation/${txn.booking_id}`}
                className="btn-accent w-full text-center flex items-center justify-center gap-1.5"
              >
                Lihat Konfirmasi <ArrowRight className="w-4 h-4" />
              </Link>
            </div>
          )}

          {txn.status === "expired" && (
            <div className="card space-y-3">
              <div className="flex items-start gap-2.5 text-sm font-bold text-[#8A6400]">
                <AlertTriangle className="w-[18px] h-[18px] flex-shrink-0 mt-0.5" />
                Pembayaran kedaluwarsa — silakan buat reservasi baru.
              </div>
              <Link href="/" className="btn-outline w-full text-center text-sm">
                Kembali ke daftar event
              </Link>
            </div>
          )}
        </div>

        {/* Right: what is being paid for. */}
        <div className="card space-y-4">
          <div className="flex items-baseline justify-between gap-2">
            <p className="font-[family-name:var(--font-display)] text-lg text-[#16224A]">
              Pesanan
            </p>
            <span className="text-[11px] font-extrabold uppercase tracking-[0.06em] text-[#2450D9]">
              #{txn.booking_id.slice(0, 8).toUpperCase()}
            </span>
          </div>

          <div className="space-y-2.5 text-[13px]">
            <div className="flex justify-between gap-4">
              <span className="text-[#6E7B96]">Event</span>
              <span className="font-semibold text-[#16224A] text-right">
                {booking?.event_title || "—"}
              </span>
            </div>
            {booking?.event_venue && (
              <div className="flex justify-between gap-4">
                <span className="text-[#6E7B96]">Lokasi</span>
                <span className="font-semibold text-[#16224A] text-right">
                  {booking.event_venue}
                </span>
              </div>
            )}
            <div className="flex justify-between gap-4">
              <span className="text-[#6E7B96]">Tiket</span>
              <span className="font-semibold text-[#16224A] text-right">
                {booking
                  ? `${totalQty} × ${booking.items.map((i) => i.ticket_type_name || "Tiket").join(", ")}`
                  : "—"}
              </span>
            </div>
            <div className="border-t border-[#E4EAF4] pt-2.5 flex justify-between gap-4">
              <span className="text-[#6E7B96]">Total</span>
              <span className="font-extrabold text-[#16224A]">
                {fmtIDR(txn.amount_rupiah)}
              </span>
            </div>
            <div className="flex justify-between gap-4">
              <span className="text-[#6E7B96]">Mata uang</span>
              <span className="font-semibold text-[#16224A]">{txn.currency}</span>
            </div>
          </div>

          {isOpen && !payExpired && (
            <ol className="space-y-2.5 border-t border-[#E4EAF4] pt-4">
              {[
                'Klik "Buka Halaman Pembayaran" — terbuka di tab baru',
                "Bayar di Xendit — pilih QRIS / VA / E-Wallet",
                "Tab ini berubah jadi konfirmasi otomatis",
              ].map((step, i) => (
                <li key={step} className="flex items-start gap-2.5">
                  <span
                    className={`flex-shrink-0 w-5 h-5 rounded-full grid place-items-center text-[10px] font-extrabold ${
                      i === 0
                        ? "bg-[#2450D9] text-white"
                        : "bg-[#EDF1F7] text-[#6E7B96]"
                    }`}
                  >
                    {i + 1}
                  </span>
                  <span className="text-[12.5px] leading-relaxed text-[#3D4A66]">
                    {step}
                  </span>
                </li>
              ))}
            </ol>
          )}

          <div className="flex items-start gap-2.5 rounded-xl bg-[#F6F9FC] px-3.5 py-3">
            <Lock className="w-4 h-4 flex-shrink-0 text-[#6E7B96] mt-0.5" />
            <p className="text-[12px] text-[#6E7B96] leading-relaxed">
              Status diperiksa tiap 3 detik — webhook pembayaran yang menentukan
              hasil akhir.
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}

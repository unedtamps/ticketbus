"use client";
import Link from "next/link";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Wallet } from "lucide-react";
import { adminApi } from "@/lib/admin-api";
import { DashboardSidebar } from "@/components/layout/sidebar";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import { fmtIDR } from "@/lib/format";
import { channelLabel, maskedAccount } from "@/lib/refund";

// Admin payout queue: the newest attempt of every event that has one. Only a
// failed attempt can be retried — a paid_out one already moved the money, and a
// pending one is still in flight. The backend enforces the same rule (409).
export default function AdminPayoutsPage() {
  const { user, hydrated, isAdmin } = useAuth();
  const queryClient = useQueryClient();

  const payoutsQuery = useQuery({
    queryKey: ["adminPayouts"],
    queryFn: () => adminApi.listPayouts(),
    enabled: !!user && isAdmin,
  });
  const payouts = payoutsQuery.data ?? [];

  const retryMutation = useMutation({
    mutationFn: (eventID: string) => adminApi.retryPayout(eventID),
    onSuccess: () => {
      toast.success("Payout dikembalikan ke antrean");
      queryClient.invalidateQueries({ queryKey: ["adminPayouts"] });
    },
    onError: (err: Error) => toast.error(err.message),
  });

  if (!hydrated) return null;
  if (!user) return null;
  // A customer or organizer reaching an admin URL sees the dashboard instead
  // of a blank page: the guard is a redirect, not a silent null.
  if (!isAdmin) {
    return (
      <div className="max-w-2xl mx-auto py-20 text-center">
        <p className="text-[15px] font-bold text-[#16224A] mb-2">Halaman khusus admin</p>
        <p className="text-sm text-[#6E7B96] mb-4">Akun kamu tidak punya akses ke halaman ini.</p>
        <Link href="/dashboard" className="text-[#2450D9] text-sm font-bold hover:underline">
          Kembali ke dashboard &rarr;
        </Link>
      </div>
    );
  }

  const failedCount = payouts.filter((p) => p.status === "failed").length;
  const paidOutTotal = payouts
    .filter((p) => p.status === "paid_out")
    .reduce((sum, p) => sum + p.amount_rupiah, 0);

  return (
    <div className="max-w-5xl mx-auto flex flex-col lg:flex-row gap-8 items-start">
      <DashboardSidebar />

      <div className="flex-1 w-full min-w-0">
        <h1 className="font-[family-name:var(--font-display)] text-4xl text-[#16224A] mb-2">
          Payouts
        </h1>
        <p className="text-[15px] text-[#6E7B96] mb-6">
          Pencairan ke penyelenggara. Attempt terbaru per event — yang gagal bisa
          dikirim ulang ke rekening EO yang tersimpan.
        </p>

        <div className="grid gap-4 sm:grid-cols-3 mb-7">
          <div className="card p-4">
            <p className="text-2xl font-extrabold text-[#16224A] tabular-nums">{payouts.length}</p>
            <p className="text-xs font-semibold text-[#6E7B96]">Event dengan payout</p>
          </div>
          <div className="card p-4">
            <p className="text-2xl font-extrabold text-[#B3362A] tabular-nums">{failedCount}</p>
            <p className="text-xs font-semibold text-[#6E7B96]">Perlu ditindaklanjuti</p>
          </div>
          <div className="card p-4">
            <p className="text-2xl font-extrabold text-[#1B7A48] tabular-nums">{fmtIDR(paidOutTotal)}</p>
            <p className="text-xs font-semibold text-[#6E7B96]">Sudah dibayar</p>
          </div>
        </div>

        {payoutsQuery.isLoading && (
          <div className="card text-center py-10">
            <Loader2 className="w-5 h-5 text-[#C9D4E8] animate-spin mx-auto mb-2" />
            <p className="text-[#6E7B96] text-sm">Memuat payout...</p>
          </div>
        )}

        {payoutsQuery.isError && (
          <div className="card text-center py-10">
            <p className="text-[#E05B4A] text-sm font-semibold">Gagal memuat payout</p>
            <button onClick={() => payoutsQuery.refetch()} className="mt-2 text-[#2450D9] text-sm font-bold hover:underline">
              Coba lagi
            </button>
          </div>
        )}

        {!payoutsQuery.isLoading && !payoutsQuery.isError && payouts.length === 0 && (
          <div className="card text-center py-12">
            <Wallet className="w-8 h-8 text-[#C9D4E8] mx-auto mb-3" />
            <p className="text-[#6E7B96] text-sm">Belum ada payout</p>
          </div>
        )}

        {!payoutsQuery.isLoading && !payoutsQuery.isError && payouts.length > 0 && (
          <div className="rounded-xl border border-[#E4EAF4] overflow-hidden bg-white">
            <div className="grid grid-cols-[1fr_140px_130px_90px] gap-3 items-center px-4 py-2.5 bg-[#F6F9FC] border-b border-[#E4EAF4]">
              <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC]">Event / Tujuan</span>
              <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC] text-right">Nominal</span>
              <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC]">Status</span>
              <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC] text-right">Aksi</span>
            </div>

            {payouts.map((p) => {
              // A retry only makes sense for a failed attempt that still has
              // money to send. Zero collectable is the no_earnings case — the
              // gateway was never called, and another attempt would fail the
              // same way.
              //
              // Written as "not a positive number" on purpose: an older backend
              // that omits the field yields undefined, and `undefined <= 0` is
              // false — which would wrongly enable Retry. This fails closed.
              const nothingToPay = !(p.collectable_rupiah > 0);
              const canRetry = p.status === "failed" && !nothingToPay;
              return (
                <div
                  key={`${p.event_id}-${p.attempt}`}
                  className={`grid grid-cols-[1fr_140px_130px_90px] gap-3 items-center px-4 py-3.5 border-b border-[#E4EAF4] last:border-b-0 border-l-[3px] ${
                    p.status === "paid_out" ? "border-l-[#2E9E5B]" :
                    p.status === "failed" ? "border-l-[#E05B4A]" : "border-l-[#FFD43B]"
                  }`}
                >
                  <div className="min-w-0">
                    <p className="truncate text-[13px] font-bold text-[#16224A]">
                      {p.event_title || p.event_id.slice(0, 8)}
                    </p>
                    <p className="truncate text-[11.5px] font-semibold text-[#6E7B96]">
                      {channelLabel(p.bank_code)} {maskedAccount(p.bank_account_number)} · attempt {p.attempt}
                    </p>
                    {nothingToPay && p.status === "failed" ? (
                      <p className="mt-0.5 text-[10.5px] font-semibold text-[#8A6400]">
                        Tidak ada dana untuk dicairkan — retry tidak akan mengirim apa pun
                      </p>
                    ) : (
                      p.failure_reason && (
                        <p className="mt-0.5 text-[10.5px] font-semibold text-[#B3362A]">{p.failure_reason}</p>
                      )
                    )}
                  </div>

                  <p className="text-right text-[13px] font-extrabold tabular-nums text-[#16224A]">
                    {fmtIDR(p.amount_rupiah)}
                  </p>

                  <span className={`badge ${
                    p.status === "paid_out" ? "badge-green" :
                    p.status === "failed" ? "badge-red" : "badge-yellow"
                  }`}>{p.status}</span>

                  <div className="flex justify-end">
                    {/* A disabled accent button still reads as "click me" at
                        50% opacity on this deep blue, so an unavailable retry
                        switches to the quiet outline style instead. */}
                    <button
                      onClick={() => retryMutation.mutate(p.event_id)}
                      disabled={!canRetry || retryMutation.isPending}
                      title={
                        p.status === "paid_out"
                          ? "Payout sudah dibayar"
                          : nothingToPay
                            ? "Tidak ada dana untuk dicairkan"
                            : p.status === "pending"
                              ? "Payout masih diproses"
                              : "Buat attempt payout baru"
                      }
                      className={`text-xs px-3 py-1.5 disabled:cursor-not-allowed ${
                        canRetry
                          ? "btn-accent disabled:opacity-40"
                          : "btn-outline opacity-40"
                      }`}
                    >
                      Retry
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

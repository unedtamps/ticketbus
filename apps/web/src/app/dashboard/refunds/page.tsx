"use client";
import Link from "next/link";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { History, Loader2 } from "lucide-react";
import { adminRefundApi, type AdminRefund } from "@/lib/admin-api";
import { DashboardSidebar } from "@/components/layout/sidebar";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import { fmtIDR } from "@/lib/format";
import { REFUND_STATUS_LABEL, refundBadgeStyle } from "@/lib/refund";

// Admin refund queue: every refund the executor has recorded, with a Retry for
// the ones that failed. There is no manual reject — the gateway owns the
// outcome, and an admin-set "failed" would tell the customer to fix an account
// their bank never rejected.
export default function AdminRefundsPage() {
  const { user, hydrated, isAdmin } = useAuth();
  const queryClient = useQueryClient();

  const refundsQuery = useQuery({
    queryKey: ["adminRefunds"],
    queryFn: () => adminRefundApi.list(""),
    enabled: !!user && isAdmin,
  });
  const refunds = refundsQuery.data ?? [];

  const retryMutation = useMutation({
    mutationFn: (id: string) => adminRefundApi.retry(id),
    onSuccess: () => {
      toast.success("Refund dikembalikan ke antrean");
      queryClient.invalidateQueries({ queryKey: ["adminRefunds"] });
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

  const failedCount = refunds.filter((r) => r.status === "failed").length;

  return (
    <div className="max-w-5xl mx-auto flex flex-col lg:flex-row gap-8 items-start">
      <DashboardSidebar />

      <div className="flex-1 w-full min-w-0">
        <h1 className="font-[family-name:var(--font-display)] text-4xl text-[#16224A] mb-2">
          Refunds
        </h1>
        <p className="text-[15px] text-[#6E7B96] mb-6">
          Refund pelanggan. Status ditentukan oleh gateway — admin hanya bisa
          mengirim ulang yang gagal.
        </p>

        <div className="grid gap-4 sm:grid-cols-3 mb-7">
          <div className="card p-4">
            <p className="text-2xl font-extrabold text-[#16224A] tabular-nums">{refunds.length}</p>
            <p className="text-xs font-semibold text-[#6E7B96]">Total refund</p>
          </div>
          <div className="card p-4">
            <p className="text-2xl font-extrabold text-[#B3362A] tabular-nums">{failedCount}</p>
            <p className="text-xs font-semibold text-[#6E7B96]">Perlu ditindaklanjuti</p>
          </div>
          <div className="card p-4">
            <p className="text-2xl font-extrabold text-[#2450D9] tabular-nums">
              {refunds.filter((r) => r.status === "succeeded").length}
            </p>
            <p className="text-xs font-semibold text-[#6E7B96]">Sudah dikirim</p>
          </div>
        </div>

        {refundsQuery.isLoading && (
          <div className="card text-center py-10">
            <Loader2 className="w-5 h-5 text-[#C9D4E8] animate-spin mx-auto mb-2" />
            <p className="text-[#6E7B96] text-sm">Memuat refund...</p>
          </div>
        )}

        {refundsQuery.isError && (
          <div className="card text-center py-10">
            <p className="text-[#E05B4A] text-sm font-semibold">Gagal memuat refund</p>
            <button onClick={() => refundsQuery.refetch()} className="mt-2 text-[#2450D9] text-sm font-bold hover:underline">
              Coba lagi
            </button>
          </div>
        )}

        {!refundsQuery.isLoading && !refundsQuery.isError && refunds.length === 0 && (
          <div className="card text-center py-12">
            <History className="w-8 h-8 text-[#C9D4E8] mx-auto mb-3" />
            <p className="text-[#6E7B96] text-sm">Belum ada refund</p>
          </div>
        )}

        {!refundsQuery.isLoading && !refundsQuery.isError && refunds.length > 0 && (
          <div className="rounded-xl border border-[#E4EAF4] overflow-hidden bg-white">
            <div className="grid grid-cols-[1fr_140px_140px_100px] gap-3 items-center px-4 py-2.5 bg-[#F6F9FC] border-b border-[#E4EAF4]">
              <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC]">Booking / Email</span>
              <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC] text-right">Nominal</span>
              <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC]">Status</span>
              <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC] text-right">Aksi</span>
            </div>

            {refunds.map((r: AdminRefund) => {
              const canRetry = r.has_destination && r.status !== "succeeded";
              return (
                <div
                  key={r.id}
                  className={`grid grid-cols-[1fr_140px_140px_100px] gap-3 items-center px-4 py-3.5 border-b border-[#E4EAF4] last:border-b-0 border-l-[3px] ${
                    r.status === "succeeded" ? "border-l-[#2E9E5B]" :
                    r.status === "failed" ? "border-l-[#E05B4A]" :
                    r.status === "processing" ? "border-l-[#2450D9]" : "border-l-[#FFD43B]"
                  }`}
                >
                  <div className="min-w-0">
                    <p className="truncate text-[13px] font-bold text-[#16224A]">
                      #{r.booking_id.slice(0, 8)}
                    </p>
                    <p className="truncate text-[11.5px] font-semibold text-[#6E7B96]">{r.customer_email}</p>
                    {!r.has_destination && (
                      <p className="mt-0.5 text-[10.5px] font-bold text-[#8A6400]">
                        Rekening refund belum diatur
                      </p>
                    )}
                    {r.has_destination && r.failure_reason && (
                      <p className="mt-0.5 text-[10.5px] font-semibold text-[#B3362A]">{r.failure_reason}</p>
                    )}
                  </div>

                  <p className="text-right text-[13px] font-extrabold tabular-nums text-[#16224A]">
                    {fmtIDR(r.amount_rupiah)}
                  </p>

                  <span className="badge" style={refundBadgeStyle(r.status)}>
                    {REFUND_STATUS_LABEL[r.status]}
                  </span>

                  <div className="flex justify-end">
                    {/* An unavailable retry switches to the quiet outline
                        style: a disabled accent button still reads as
                        clickable on this deep blue. */}
                    <button
                      onClick={() => retryMutation.mutate(r.id)}
                      disabled={!canRetry || retryMutation.isPending}
                      title={
                        !r.has_destination
                          ? "Customer belum mengatur rekening refund"
                          : r.status === "succeeded"
                            ? "Refund sudah berhasil"
                            : "Kirim ulang payout refund"
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

        <p className="mt-4 text-[11.5px] text-[#9AA6BC]">
          Rekening tujuan diatur pelanggan sendiri di akun mereka. Refund tanpa
          rekening tetap menunggu dan tidak bisa dikirim ulang.
        </p>
      </div>
    </div>
  );
}

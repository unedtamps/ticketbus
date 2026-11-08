"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Wallet, Loader2 } from "lucide-react";
import { api } from "@/lib/api-client";
import { DashboardSidebar } from "@/components/layout/sidebar";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";

// Refund destinations: banks and e-wallets the Payouts API can send to.
// E-wallet destinations use the registered phone number as the account.
const CHANNELS = [
  { code: "BCA", label: "Bank BCA", type: "bank" },
  { code: "BNI", label: "Bank BNI", type: "bank" },
  { code: "BRI", label: "Bank BRI", type: "bank" },
  { code: "MANDIRI", label: "Bank Mandiri", type: "bank" },
  { code: "PERMATA", label: "Bank Permata", type: "bank" },
  { code: "CIMB", label: "Bank CIMB", type: "bank" },
  { code: "GOPAY", label: "GoPay", type: "ewallet" },
  { code: "OVO", label: "OVO", type: "ewallet" },
  { code: "DANA", label: "DANA", type: "ewallet" },
  { code: "SHOPEEPAY", label: "ShopeePay", type: "ewallet" },
  { code: "LINKAJA", label: "LinkAja", type: "ewallet" },
] as const;

interface RefundAccount {
  channel_code: string;
  account_number: string;
  account_holder_name: string;
  updated_at?: string;
}

export default function RefundAccountPage() {
  const { user, hydrated } = useAuth();
  const queryClient = useQueryClient();

  const accountQuery = useQuery({
    queryKey: ["refundAccount"],
    queryFn: () =>
      api.get<{ refund_account: RefundAccount | null }>("/api/refund-account"),
    enabled: !!user,
    retry: false,
  });
  const account = accountQuery.data?.refund_account ?? null;

  const saveMutation = useMutation({
    mutationFn: (values: {
      channel_code: string;
      account_number: string;
      account_holder_name: string;
    }) => api.put("/api/refund-account", values),
    onSuccess: () => {
      toast.success("Rekening refund tersimpan");
      queryClient.invalidateQueries({ queryKey: ["refundAccount"] });
    },
    onError: (err: Error) => toast.error(err.message),
  });

  if (!hydrated) return null;

  return (
    <div className="max-w-5xl mx-auto flex flex-col lg:flex-row gap-8 items-start">
      <DashboardSidebar />

      <div className="flex-1 w-full">
        <h1 className="font-[family-name:var(--font-display)] text-4xl text-[#16224A] mb-2">
          Rekening Refund
        </h1>
        <p className="text-[15px] text-[#6E7B96] mb-6">
          Jika event yang kamu beli dibatalkan, refund dikirim ke rekening di
          bawah ini — cukup daftar sekali.
        </p>

        {accountQuery.isLoading ? (
          <div className="card flex items-center gap-3 py-10 justify-center">
            <Loader2 className="w-5 h-5 text-[#C9D4E8] animate-spin" />
            <span className="text-sm text-[#6E7B96]">Memuat…</span>
          </div>
        ) : (
          <RefundAccountForm
            account={account}
            defaultHolderName={user?.name ?? ""}
            saving={saveMutation.isPending}
            onSave={(values) => saveMutation.mutate(values)}
          />
        )}
      </div>
    </div>
  );
}

function RefundAccountForm({
  account,
  defaultHolderName,
  saving,
  onSave,
}: {
  account: RefundAccount | null;
  defaultHolderName: string;
  saving: boolean;
  onSave: (values: {
    channel_code: string;
    account_number: string;
    account_holder_name: string;
  }) => void;
}) {
  const [channel, setChannel] = useState(account?.channel_code ?? "BCA");
  const [accountNumber, setAccountNumber] = useState(
    account?.account_number ?? "",
  );
  const [holderName, setHolderName] = useState(
    account?.account_holder_name ?? defaultHolderName,
  );

  // Re-seed the form when the saved account arrives after loading.
  useEffect(() => {
    if (account) {
      setChannel(account.channel_code);
      setAccountNumber(account.account_number);
      setHolderName(account.account_holder_name);
    }
  }, [account]);

  const channelType = CHANNELS.find((c) => c.code === channel)?.type ?? "bank";
  const numberLabel = channelType === "ewallet" ? "Nomor HP" : "Nomor Rekening";
  const numberValid =
    channelType === "ewallet"
      ? /^08\d{6,13}$/.test(accountNumber.trim())
      : /^\d{5,20}$/.test(accountNumber.trim());
  const valid = numberValid && holderName.trim().length > 0;

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!valid) return;
    onSave({
      channel_code: channel,
      account_number: accountNumber.trim(),
      account_holder_name: holderName.trim(),
    });
  }

  return (
    <form
      onSubmit={handleSubmit}
      className="card p-6 space-y-5 max-w-xl"
      data-testid="refund-account-form"
    >
      <div className="flex items-center gap-2.5">
        <span className="icon-tile">
          <Wallet className="w-5 h-5" />
        </span>
        <div>
          <p className="text-sm font-bold text-[#16224A]">Tujuan Refund</p>
          <p className="text-xs text-[#9AA6BC]">
            {account
              ? `Terdaftar — diperbarui terakhir ${new Date(account.updated_at ?? Date.now()).toLocaleDateString("id-ID")}`
              : "Belum diatur"}
          </p>
        </div>
      </div>

      <div>
        <label className="block text-xs font-semibold text-[#6E7B96] mb-1">
          Bank / E-Wallet
        </label>
        <select
          value={channel}
          onChange={(e) => setChannel(e.target.value)}
          className="input-field"
        >
          {CHANNELS.map((c) => (
            <option key={c.code} value={c.code}>
              {c.label}
            </option>
          ))}
        </select>
      </div>

      <div>
        <label className="block text-xs font-semibold text-[#6E7B96] mb-1">
          {numberLabel}
        </label>
        <input
          type="text"
          inputMode="numeric"
          value={accountNumber}
          onChange={(e) =>
            setAccountNumber(e.target.value.replace(/[^\d]/g, ""))
          }
          className="input-field"
          placeholder={
            channelType === "ewallet" ? "081234567890" : "1234567890"
          }
          maxLength={20}
        />
      </div>

      <div>
        <label className="block text-xs font-semibold text-[#6E7B96] mb-1">
          Nama Pemilik Rekening
        </label>
        <input
          type="text"
          value={holderName}
          onChange={(e) => setHolderName(e.target.value)}
          className="input-field"
          placeholder="Sesuai catatan bank / e-wallet"
        />
        <p className="text-[11px] text-[#8A6400] mt-1.5">
          Harus sama persis dengan nama yang terdaftar — jika tidak cocok,
          payout ditolak oleh bank.
        </p>
      </div>

      <button
        type="submit"
        disabled={saving || !valid}
        className="btn-accent w-full py-3"
      >
        {saving ? (
          <Loader2 className="w-4 h-4 animate-spin mx-auto" />
        ) : account ? (
          "Perbarui Rekening"
        ) : (
          "Simpan Rekening"
        )}
      </button>
    </form>
  );
}

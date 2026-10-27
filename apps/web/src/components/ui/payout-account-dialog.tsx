"use client";

import { useState } from "react";
import { Building2, Loader2, X } from "lucide-react";
import type { PayoutAccount } from "@/types";

const BANKS = ["BCA", "BNI", "BRI", "MANDIRI", "PERMATA", "CIMB", "MAYBANK"];

interface PayoutAccountDialogProps {
  account?: PayoutAccount;
  loading?: boolean;
  onSubmit: (values: {
    bank_code: string;
    bank_account_number: string;
    bank_account_holder_name: string;
  }) => void;
  onClose: () => void;
}

export function PayoutAccountDialog({
  account,
  loading = false,
  onSubmit,
  onClose,
}: PayoutAccountDialogProps) {
  const [bankCode, setBankCode] = useState(account?.bank_code || "BCA");
  const [accountNumber, setAccountNumber] = useState(
    account?.bank_account_number || "",
  );
  const [holderName, setHolderName] = useState(
    account?.bank_account_holder_name || "",
  );
  const [error, setError] = useState("");

  const valid =
    bankCode !== "" &&
    /^\d{5,20}$/.test(accountNumber.trim()) &&
    holderName.trim().length > 0;

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!valid) {
      setError("Enter a valid bank, 5–20 digit account number and holder name.");
      return;
    }
    setError("");
    onSubmit({
      bank_code: bankCode,
      bank_account_number: accountNumber.trim(),
      bank_account_holder_name: holderName.trim(),
    });
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div
        className="absolute inset-0 bg-[#1A1817]/40 backdrop-blur-sm"
        onClick={onClose}
      />
      <form
        onSubmit={handleSubmit}
        className="relative card w-full max-w-sm space-y-4 shadow-xl"
      >
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Building2 className="w-5 h-5 text-[#D9381E]" />
            <p className="font-[family-name:var(--font-display)] text-lg text-[#1A1817]">
              Payout Account
            </p>
          </div>
          <button type="button" onClick={onClose} className="btn-ghost p-1">
            <X className="w-4 h-4" />
          </button>
        </div>

        <p className="text-sm text-[#8B8580]">
          Earnings from your events are sent to this bank account.
        </p>

        <div>
          <label className="block text-xs text-[#8B8580] mb-1">Bank</label>
          <select
            value={bankCode}
            onChange={(e) => setBankCode(e.target.value)}
            className="input-field"
          >
            {BANKS.map((b) => (
              <option key={b} value={b}>
                {b}
              </option>
            ))}
          </select>
        </div>

        <div>
          <label className="block text-xs text-[#8B8580] mb-1">
            Account Number
          </label>
          <input
            type="text"
            inputMode="numeric"
            value={accountNumber}
            onChange={(e) => setAccountNumber(e.target.value.replace(/\D/g, ""))}
            className="input-field"
            placeholder="e.g. 1234567890"
            maxLength={20}
          />
        </div>

        <div>
          <label className="block text-xs text-[#8B8580] mb-1">
            Account Holder Name
          </label>
          <input
            type="text"
            value={holderName}
            onChange={(e) => setHolderName(e.target.value)}
            className="input-field"
            placeholder="Name as it appears on the account"
          />
        </div>

        {error && <p className="text-xs text-[#D9381E]">{error}</p>}

        <div className="flex gap-3 justify-end pt-2">
          <button
            type="button"
            onClick={onClose}
            className="btn-ghost text-sm"
            disabled={loading}
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={loading || !valid}
            className="btn-accent text-sm disabled:opacity-50"
          >
            {loading ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : (
              "Save Account"
            )}
          </button>
        </div>
      </form>
    </div>
  );
}

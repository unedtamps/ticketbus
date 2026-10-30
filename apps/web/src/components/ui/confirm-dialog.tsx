"use client";

import { useState } from "react";
import { AlertTriangle, Check, X } from "lucide-react";

interface ConfirmDialogProps {
  open: boolean;
  title: string;
  message?: string;
  confirmLabel: string;
  variant?: "danger" | "success" | "warning";
  loading?: boolean;
  requireReason?: boolean;
  reasonLabel?: string;
  reasonPlaceholder?: string;
  onConfirm: (reason?: string) => void;
  onClose: () => void;
}

export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel,
  variant = "danger",
  loading = false,
  requireReason = false,
  reasonLabel = "Reason",
  reasonPlaceholder = "e.g. Insufficient details...",
  onConfirm,
  onClose,
}: ConfirmDialogProps) {
  const [reason, setReason] = useState("");

  if (!open) return null;

  const canSubmit = !requireReason || reason.trim().length > 0;

  const variantClasses =
    variant === "danger"
      ? "bg-[#16224A] text-white hover:opacity-85"
      : variant === "warning"
        ? "bg-[#FFD43B] text-[#16224A] hover:bg-[#F0C419]"
        : "bg-[#2450D9] text-white hover:bg-[#1D3FAE]";

  const icon =
    variant === "danger" ? (
      <AlertTriangle className="w-5 h-5 text-[#16224A]" />
    ) : variant === "warning" ? (
      <AlertTriangle className="w-5 h-5 text-[#8A6400]" />
    ) : (
      <Check className="w-5 h-5 text-[#2450D9]" />
    );

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-[#16224A]/40 backdrop-blur-sm" onClick={onClose} />
      <div className="relative card w-full max-w-sm space-y-4 shadow-xl">
        <div className="flex items-center gap-2">
          {icon}
          <p className="font-[family-name:var(--font-display)] text-lg text-[#16224A]">{title}</p>
        </div>
        {message ? <p className="text-sm text-[#6E7B96]">{message}</p> : null}
        {requireReason && (
          <div>
            <label className="block text-xs text-[#6E7B96] mb-1">{reasonLabel}</label>
            <textarea
              required
              value={reason}
              onChange={e => setReason(e.target.value)}
              className="input-field"
              rows={3}
              placeholder={reasonPlaceholder}
            />
          </div>
        )}
        <div className="flex gap-3 justify-end pt-2">
          <button onClick={onClose} className="btn-ghost text-sm">
            Cancel
          </button>
          <button
            onClick={() => onConfirm(requireReason ? reason : undefined)}
            disabled={loading || !canSubmit}
            className={`text-sm flex items-center gap-1.5 px-4 py-2 rounded-xl font-bold transition-all duration-150 disabled:opacity-50 disabled:cursor-not-allowed ${variantClasses}`}
          >
            {variant === "danger" && <X className="w-4 h-4" />}
            {variant === "success" && <Check className="w-4 h-4" />}
            {loading ? "Please wait..." : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}

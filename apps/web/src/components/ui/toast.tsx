"use client";

import React, { createContext, useContext, useState, useCallback, useEffect, useRef } from "react";
import { X, AlertCircle, CheckCircle2, Info } from "lucide-react";

type ToastType = "error" | "success" | "info";

interface Toast {
  id: string;
  message: string;
  type: ToastType;
}

interface ToastContextValue {
  show: (message: string, type: ToastType) => void;
  error: (message: string) => void;
  success: (message: string) => void;
  info: (message: string) => void;
}

const ToastContext = createContext<ToastContextValue>({
  show: () => {},
  error: () => {},
  success: () => {},
  info: () => {},
});

let counter = 0;
let globalShow: ((message: string, type: ToastType) => void) | null = null;

export function useToast() {
  return useContext(ToastContext);
}

export const toast = {
  show(message: string, type: ToastType) {
    globalShow?.(message, type);
  },
  error(message: string) { this.show(message, "error"); },
  success(message: string) { this.show(message, "success"); },
  info(message: string) { this.show(message, "info"); },
};

const iconMap: Record<ToastType, React.ReactNode> = {
  error: <AlertCircle className="w-4 h-4 text-[#D9381E]" />,
  success: <CheckCircle2 className="w-4 h-4 text-[#2D7A46]" />,
  info: <Info className="w-4 h-4 text-[#1A5DB8]" />,
};

const borderMap: Record<ToastType, string> = {
  error: "border-l-[#D9381E]",
  success: "border-l-[#2D7A46]",
  info: "border-l-[#1A5DB8]",
};

function ToastBar({ toast, onDismiss }: { toast: Toast; onDismiss: (id: string) => void }) {
  const timerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => {
    timerRef.current = setTimeout(() => onDismiss(toast.id), 5000);
    return () => { if (timerRef.current) clearTimeout(timerRef.current); };
  }, [toast.id, onDismiss]);

  return (
    <div
      className={`pointer-events-auto flex items-start gap-3 w-full max-w-sm bg-white border border-[#E8E3DC] border-l-4 ${borderMap[toast.type]} rounded-md shadow-lg p-4 animate-slide-down`}
      role="alert"
    >
      <div className="flex-shrink-0 mt-0.5">{iconMap[toast.type]}</div>
      <p className="flex-1 text-sm text-[#4A4541] leading-snug">{toast.message}</p>
      <button
        onClick={() => onDismiss(toast.id)}
        className="flex-shrink-0 text-[#B0A89E] hover:text-[#1A1817] transition-colors"
      >
        <X className="w-4 h-4" />
      </button>
    </div>
  );
}

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const dismiss = useCallback((id: string) => {
    setToasts(prev => prev.filter(t => t.id !== id));
  }, []);

  const show = useCallback((message: string, type: ToastType) => {
    const id = String(++counter);
    setToasts(prev => [...prev, { id, message, type }]);
  }, []);

  useEffect(() => {
    globalShow = show;
    return () => { globalShow = null; };
  }, [show]);

  const value: ToastContextValue = {
    show,
    error: useCallback((m: string) => show(m, "error"), [show]),
    success: useCallback((m: string) => show(m, "success"), [show]),
    info: useCallback((m: string) => show(m, "info"), [show]),
  };

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div
        aria-live="polite"
        className="fixed bottom-0 left-1/2 -translate-x-1/2 z-[100] pb-4 flex flex-col items-center gap-2 pointer-events-none"
      >
        {toasts.map(t => (
          <ToastBar key={t.id} toast={t} onDismiss={dismiss} />
        ))}
      </div>
    </ToastContext.Provider>
  );
}

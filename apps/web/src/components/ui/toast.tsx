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
  error: <AlertCircle className="w-4 h-4 text-[#8A6400]" />,
  success: <CheckCircle2 className="w-4 h-4 text-[#2450D9]" />,
  info: <Info className="w-4 h-4 text-[#2450D9]" />,
};

const borderMap: Record<ToastType, string> = {
  error: "border-l-[#FFD43B]",
  success: "border-l-[#2450D9]",
  info: "border-l-[#2450D9]",
};

function ToastBar({ toast, onDismiss }: { toast: Toast; onDismiss: (id: string) => void }) {
  const timerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => {
    timerRef.current = setTimeout(() => onDismiss(toast.id), 5000);
    return () => { if (timerRef.current) clearTimeout(timerRef.current); };
  }, [toast.id, onDismiss]);

  return (
    <div
      className={`pointer-events-auto flex items-start gap-3 w-full max-w-sm bg-white border border-[#E4EAF4] border-l-4 ${borderMap[toast.type]} rounded-xl shadow-lg p-4 animate-toast-in`}
      role="alert"
    >
      <div className="flex-shrink-0 mt-0.5">{iconMap[toast.type]}</div>
      <p className="flex-1 text-sm text-[#3D4A66] leading-snug">{toast.message}</p>
      <button
        onClick={() => onDismiss(toast.id)}
        className="flex-shrink-0 text-[#9AA6BC] hover:text-[#16224A] transition-colors"
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
    setToasts([{ id, message, type }]);
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
        style={{
          position: "fixed",
          top: "1rem",
          left: "50%",
          transform: "translateX(-50%)",
          zIndex: 100,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          gap: "0.5rem",
          pointerEvents: "none",
        }}
      >
        {toasts.map(t => (
          <ToastBar key={t.id} toast={t} onDismiss={dismiss} />
        ))}
      </div>
    </ToastContext.Provider>
  );
}

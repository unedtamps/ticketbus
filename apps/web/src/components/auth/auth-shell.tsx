"use client";

import type { ComponentType, ReactNode } from "react";

// Two-panel auth layout shared by /login and /register: image panel with a pull
// quote on the left, form on the right. The image panel is hidden below lg so
// the form keeps the full width on small screens.
//
// The shell still escapes the root layout's `container py-10` so it can use the
// full window width; w-screen + left-1/2/-translate-x-1/2 cancel the max-width
// and the negative vertical margin cancels the shared padding. It then insets
// itself with a slim margin on all sides.
//
// Height is pinned to the viewport minus the header (3.5rem plus its 1px bottom
// border) and minus the vertical margins, so a long form — register with an
// organizer profile — scrolls inside the form column instead of stretching the
// page.
const AUTH_MARGIN = "1rem";

export function AuthShell({ children }: { children: ReactNode }) {
  return (
    <div className="relative left-1/2 -my-10 w-screen -translate-x-1/2 px-4 py-4">
      <div
        className="grid overflow-hidden rounded-[20px] border border-[#E4EAF4] bg-white lg:grid-cols-[1fr_minmax(0,560px)]"
        style={{ height: `calc(100vh - 3.5rem - 1px - ${AUTH_MARGIN} * 2)` }}
      >
        {/* Image first so it occupies the left column. Hidden below lg. */}
        <div className="relative order-2 hidden h-full lg:order-1 lg:block">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img
            src="https://images.unsplash.com/photo-1562362840-6985a3733d25?auto=format&fit=crop&w=1600&q=80"
            alt=""
            className="absolute inset-0 h-full w-full object-cover"
          />
          <div className="absolute bottom-10 left-10 w-[420px] rounded-[18px] border border-white/85 bg-white/70 p-6 backdrop-blur-md">
            <p className="font-[family-name:var(--font-display)] text-[22px] leading-[1.25] text-[#16224A]">
              &ldquo;Satu app untuk semua tiket panggung — dari Braga sampai
              JIExpo.&rdquo;
            </p>
            <p className="mt-2.5 text-xs font-bold text-[#6E7B96]">
              — Rani, promotor indie Jakarta
            </p>
          </div>
        </div>

        <div className="order-1 flex h-full min-h-0 flex-col overflow-y-auto px-5 py-8 sm:px-8 lg:order-2">
          <div className="m-auto w-full max-w-[472px]">{children}</div>
        </div>
      </div>
    </div>
  );
}

// Wordmark shown at the top of both auth forms.
export function AuthWordmark() {
  return (
    <div className="flex items-center gap-2">
      <span className="font-[family-name:var(--font-display)] text-2xl text-[#16224A]">
        Ticketbus
      </span>
      <span className="h-2 w-2 rounded-full bg-[#FFD43B]" aria-hidden />
    </div>
  );
}

// Label + input with a leading icon. Kept local to auth so the dashboard's
// .input-field style stays untouched.
export function AuthField({
  id,
  label,
  type = "text",
  value,
  onChange,
  placeholder,
  icon: Icon,
  required,
  minLength,
  autoComplete,
  trailing,
}: {
  id: string;
  label: string;
  type?: string;
  value: string;
  onChange: (v: string) => void;
  placeholder: string;
  icon: ComponentType<{ className?: string }>;
  required?: boolean;
  minLength?: number;
  autoComplete?: string;
  trailing?: ReactNode;
}) {
  return (
    <div>
      <label
        htmlFor={id}
        className="mb-1.5 block text-[13px] font-semibold text-[#16224A]"
      >
        {label}
      </label>
      <div className="flex items-center gap-2.5 rounded-xl border border-[#E4EAF4] bg-white px-3.5 py-3 transition-colors focus-within:border-[#2450D9] focus-within:shadow-[0_0_0_3px_rgba(36,80,217,0.08)]">
        <Icon className="h-4 w-4 flex-shrink-0 text-[#9AA6BC]" />
        <input
          id={id}
          type={type}
          required={required}
          minLength={minLength}
          autoComplete={autoComplete}
          value={value}
          onChange={e => onChange(e.target.value)}
          placeholder={placeholder}
          className="min-w-0 flex-1 bg-transparent text-sm text-[#16224A] outline-none placeholder:text-[#9AA6BC]"
        />
        {trailing}
      </div>
    </div>
  );
}

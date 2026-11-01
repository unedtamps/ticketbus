"use client";

import Link from "next/link";
import { useState } from "react";
import { useAuth } from "@/lib/auth-context";
import {
  LayoutDashboard,
  Calendar,
  Wallet,
  Shield,
  ShoppingBag,
  CreditCard,
  CalendarPlus,
  type LucideIcon,
} from "lucide-react";

interface Item {
  id: string;
  label: string;
  icon: LucideIcon;
  href?: string;
}

export function DashboardSidebar() {
  const { user, isAdmin, isEO, isCustomer } = useAuth();
  const [active, setActive] = useState("dash-eo-top");

  if (!user) return null;

  let items: Item[] = [];
  if (isAdmin) {
    items = [
      { id: "dash-admin", label: "Moderasi", icon: Shield },
      { id: "dash-admin-events", label: "All Events", icon: Calendar },
    ];
  } else if (isEO) {
    items = [
      { id: "dash-eo-top", label: "Dashboard", icon: LayoutDashboard },
      { id: "dash-my-events", label: "My Events", icon: Calendar },
      { id: "dash-payout-summary", label: "Payouts", icon: Wallet },
      { id: "dash-create", label: "Create Event", icon: CalendarPlus, href: "/events" },
    ];
  } else if (isCustomer) {
    items = [
      { id: "dash-tickets", label: "My Tickets", icon: ShoppingBag },
      { id: "dash-payments", label: "Payments", icon: CreditCard },
    ];
  }

  const initials = user.name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0]!.toUpperCase())
    .join("");

  function go(item: Item) {
    setActive(item.id);
    document
      .getElementById(item.id)
      ?.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  return (
    <aside className="hidden lg:flex flex-col gap-5 w-[236px] flex-shrink-0 sticky top-[88px] bg-[#FBFCFE] border border-[#E4EAF4] rounded-2xl p-4 self-start">
      <div className="flex items-center gap-2 px-1">
        <span className="font-[family-name:var(--font-display)] text-lg text-[#16224A]">
          Ticketbus
        </span>
        <span className="w-[7px] h-[7px] rounded-full bg-[#FFD43B]" aria-hidden />
      </div>

      <nav className="flex flex-col gap-1">
        {items.map((item) => {
          const cls = `sidebar-link ${active === item.id && !item.href ? "active" : ""}`;
          return item.href ? (
            <Link key={item.id} href={item.href} className={cls}>
              <item.icon className="w-[17px] h-[17px]" />
              {item.label}
            </Link>
          ) : (
            <button key={item.id} onClick={() => go(item)} className={cls}>
              <item.icon className="w-[17px] h-[17px]" />
              {item.label}
            </button>
          );
        })}
      </nav>

      <div className="mt-2 flex items-center gap-2.5 p-3 rounded-xl bg-white border border-[#E4EAF4]">
        <span className="w-9 h-9 rounded-full bg-[#E7EEFF] text-[#2450D9] flex items-center justify-center text-xs font-extrabold flex-shrink-0">
          {initials}
        </span>
        <span className="min-w-0">
          <span className="block text-[13px] font-bold text-[#16224A] truncate">
            {user.name}
          </span>
          <span className="block text-[11px] font-semibold text-[#6E7B96] capitalize">
            {user.role}
          </span>
        </span>
      </div>
    </aside>
  );
}

"use client";

import Link from "next/link";
import { useState } from "react";
import { usePathname } from "next/navigation";
import { useAuth } from "@/lib/auth-context";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import {
  Ticket,
  LayoutDashboard,
  CalendarPlus,
  Shield,
  LogOut,
  LogIn,
  UserPlus,
} from "lucide-react";

function NavLink({ href, children, icon: Icon }: { href: string; children: React.ReactNode; icon?: React.ComponentType<{ className?: string }> }) {
  const pathname = usePathname();
  const isActive = pathname === href || (href !== "/" && pathname.startsWith(href));

  return (
    <Link
      href={href}
      className={`flex items-center gap-1.5 text-sm font-semibold px-3 py-1.5 rounded-lg transition-colors duration-150 ${
        isActive
          ? "text-[#2450D9] bg-[#E7EEFF]"
          : "text-[#3D4A66] hover:text-[#16224A] hover:bg-[#F6F9FC]"
      }`}
    >
      {Icon && <Icon className="w-4 h-4" />}
      {children}
    </Link>
  );
}

export function Header() {
  const { user, hydrated, loading, isAdmin, isEO, logout } = useAuth();
  const authPending = !hydrated || loading;
  // Logging out drops the session and redirects, so it asks first.
  const [confirmingLogout, setConfirmingLogout] = useState(false);

  return (
    <>
      <header className="sticky top-0 z-50 border-b border-[#E4EAF4] bg-white/90 backdrop-blur-sm">
      <div className="container flex items-center justify-between h-14">
        <Link href="/" className="flex items-center gap-1.5 group">
          <span className="font-[family-name:var(--font-display)] text-xl text-[#16224A] transition-colors">
            Ticketbus
          </span>
          <span className="w-[7px] h-[7px] rounded-full bg-[#FFD43B]" aria-hidden />
        </Link>

        <nav className="flex items-center gap-1">
          <NavLink href="/" icon={Ticket}>Events</NavLink>

          {authPending ? (
            <span
              className="mx-1 h-7 w-32 rounded-lg bg-[#F6F9FC] animate-pulse"
              aria-hidden
            />
          ) : user ? (
            <>
              <NavLink href="/dashboard" icon={isAdmin ? Shield : LayoutDashboard}>Dashboard</NavLink>
              {isEO && <NavLink href="/events" icon={CalendarPlus}>Create</NavLink>}
              <span className="w-px h-5 bg-[#E4EAF4] mx-1" />
              <span className="text-xs text-[#6E7B96] px-2 font-semibold">{user.name}</span>
              <button
                onClick={() => setConfirmingLogout(true)}
                className="flex items-center gap-1.5 text-sm font-semibold px-3 py-1.5 rounded-lg text-[#6E7B96] hover:text-[#16224A] hover:bg-[#F6F9FC] transition-colors duration-150"
              >
                <LogOut className="w-4 h-4" />
                Logout
              </button>
            </>
          ) : (
            <>
              <Link href="/login" className="flex items-center gap-1.5 text-sm font-semibold px-3 py-1.5 rounded-lg text-[#3D4A66] hover:text-[#16224A] hover:bg-[#F6F9FC] transition-colors duration-150">
                <LogIn className="w-4 h-4" />
                Login
              </Link>
              <Link href="/register" className="flex items-center gap-1.5 text-sm font-bold px-3 py-1.5 rounded-lg bg-[#2450D9] text-white hover:bg-[#1D3FAE] transition-colors duration-150">
                <UserPlus className="w-4 h-4" />
                Register
              </Link>
            </>
          )}
        </nav>
      </div>
    </header>

    {/* Rendered as a sibling of <header>: its backdrop-blur creates a
        containing block for fixed descendants, which would trap the overlay.
        Copy stays role-neutral: this header is shared by EO, admin and
        customer accounts. */}
    <ConfirmDialog
      open={confirmingLogout}
      title="Log out?"
      message="You will need to sign in again to continue."
      confirmLabel="Log out"
      variant="danger"
      onConfirm={() => {
        setConfirmingLogout(false);
        logout();
      }}
      onClose={() => setConfirmingLogout(false)}
    />
    </>
  );
}

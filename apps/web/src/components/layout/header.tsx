"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useAuth } from "@/lib/auth-context";
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
      className={`flex items-center gap-1.5 text-sm font-medium px-3 py-1.5 rounded-md transition-colors duration-150 ${
        isActive
          ? "text-[#D9381E] bg-[#D9381E]/6"
          : "text-[#4A4541] hover:text-[#1A1817] hover:bg-[#F5F0E8]"
      }`}
    >
      {Icon && <Icon className="w-4 h-4" />}
      {children}
    </Link>
  );
}

export function Header() {
  const { user, hydrated, isAdmin, isEO, logout } = useAuth();

  return (
    <header className="sticky top-0 z-50 border-b border-[#E8E3DC] bg-[#FEFBF6]/90 backdrop-blur-sm">
      <div className="container flex items-center justify-between h-14">
        <Link href="/" className="flex items-center gap-2 group">
          <span className="font-[family-name:var(--font-display)] text-xl text-[#D9381E] group-hover:text-[#B82E1A] transition-colors">
            TicketSaas
          </span>
        </Link>

        <nav className="flex items-center gap-1">
          <NavLink href="/" icon={Ticket}>Events</NavLink>

          {hydrated && user ? (
            <>
              <NavLink href="/dashboard" icon={LayoutDashboard}>Dashboard</NavLink>
              {(isEO || isAdmin) && (
                <NavLink href="/events" icon={CalendarPlus}>Create</NavLink>
              )}
              {isAdmin && (
                <NavLink href="/admin" icon={Shield}>Admin</NavLink>
              )}
              <span className="w-px h-5 bg-[#E8E3DC] mx-1" />
              <span className="text-xs text-[#8B8580] px-2 font-medium">{user.name}</span>
              <button
                onClick={logout}
                className="flex items-center gap-1.5 text-sm font-medium px-3 py-1.5 rounded-md text-[#8B8580] hover:text-[#D9381E] hover:bg-[#FFF5F5] transition-colors duration-150"
              >
                <LogOut className="w-4 h-4" />
                Logout
              </button>
            </>
          ) : (
            <>
              <Link href="/login" className="flex items-center gap-1.5 text-sm font-medium px-3 py-1.5 rounded-md text-[#4A4541] hover:text-[#1A1817] hover:bg-[#F5F0E8] transition-colors duration-150">
                <LogIn className="w-4 h-4" />
                Login
              </Link>
              <Link href="/register" className="flex items-center gap-1.5 text-sm font-semibold px-3 py-1.5 rounded-md bg-[#D9381E] text-white hover:bg-[#B82E1A] transition-colors duration-150">
                <UserPlus className="w-4 h-4" />
                Register
              </Link>
            </>
          )}
        </nav>
      </div>
    </header>
  );
}

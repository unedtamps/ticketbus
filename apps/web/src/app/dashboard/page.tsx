"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api } from "@/lib/api-client";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import { LayoutDashboard, CalendarPlus, Shield, ShoppingBag, Ticket, CreditCard, ChevronRight, Loader2 } from "lucide-react";
import { fmtShortDate } from "@/lib/format";
import type { EventItem, TransactionResponse } from "@/types";

interface BookingItem {
  ticket_type_id: string;
  quantity: number;
  unit_price_cents: number;
}

interface Booking {
  id: string;
  event_id: string;
  status: string;
  total_cents: number;
  created_at: string;
  items: BookingItem[];
}

export default function DashboardPage() {
  const { user, hydrated, isAdmin, isEO, isCustomer } = useAuth();
  const [bookings, setBookings] = useState<Booking[]>([]);
  const [transactions, setTransactions] = useState<TransactionResponse[]>([]);
  const [myEvents, setMyEvents] = useState<EventItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [pendingCount, setPendingCount] = useState(0);

  useEffect(() => {
    if (!user) { setLoading(false); return; }

    // Customers: load bookings. EO/admin: load own events.
    if (isCustomer) {
      api.get<Booking[]>("/api/bookings")
        .then(setBookings)
        .catch(() => { toast.info("Failed to load bookings"); setBookings([]); })
        .finally(() => setLoading(false));
      api.get<TransactionResponse[]>("/api/payments")
        .then(setTransactions)
        .catch(() => { toast.info("Failed to load payment history"); setTransactions([]); });
    } else {
      api.get<EventItem[]>("/api/events/mine")
        .then(setMyEvents)
        .catch(() => { toast.info("Failed to load your events"); setMyEvents([]); })
        .finally(() => setLoading(false));
    }

    if (isAdmin) {
      api.get<{ events: EventItem[]; total: number }>("/api/events/pending?limit=1")
        .then(d => setPendingCount(d.total))
        .catch(() => {});
    }
  }, [user, isCustomer, isAdmin]);

  if (!hydrated) return null;
  if (!user) return null;

  return (
    <div>
      {/* Header */}
      <div className="mb-10">
        <p className="font-[family-name:var(--font-display)] text-3xl text-[#1A1817] mb-1">Dashboard</p>
        <p className="text-[#8B8580] text-sm flex items-center gap-2">
          {user.name}
          <span className={`badge ${
            isAdmin ? "badge-blue" : isEO ? "badge-green" : "badge-ink"
          }`}>{user.role}</span>
        </p>
      </div>

      {/* Quick actions */}
      <div className="grid gap-4 sm:grid-cols-3 mb-12 fade-up">
        {/* Create Event — EO and Admin */}
        {isEO && (
          <Link href="/events" className="card flex items-center gap-4 group p-4">
            <div className="w-10 h-10 rounded-lg bg-[#D9381E]/6 flex items-center justify-center flex-shrink-0">
              <CalendarPlus className="w-5 h-5 text-[#D9381E]" />
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-sm font-semibold text-[#1A1817]">Create Event</p>
              <p className="text-xs text-[#8B8580] truncate">Set up a new event</p>
            </div>
            <ChevronRight className="w-4 h-4 text-[#D4CEC4] group-hover:text-[#D9381E] transition-colors flex-shrink-0" />
          </Link>
        )}

        {/* Admin Panel — Admin only */}
        {isAdmin && (
          <Link href="/admin" className="card flex items-center gap-4 group p-4">
            <div className="w-10 h-10 rounded-lg bg-[#1A5DB8]/6 flex items-center justify-center flex-shrink-0">
              <Shield className="w-5 h-5 text-[#1A5DB8]" />
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-sm font-semibold text-[#1A1817]">Admin Panel</p>
              <p className="text-xs text-[#8B8580]">
                {pendingCount > 0 ? `${pendingCount} pending` : "Manage events"}
              </p>
            </div>
            {pendingCount > 0 && <span className="badge badge-red flex-shrink-0">{pendingCount}</span>}
            <ChevronRight className="w-4 h-4 text-[#D4CEC4] group-hover:text-[#1A5DB8] transition-colors flex-shrink-0" />
          </Link>
        )}

        {/* Tickets/Events counter */}
        <div className="card flex items-center gap-4 p-4">
          <div className="w-10 h-10 rounded-lg bg-[#B85C1A]/6 flex items-center justify-center flex-shrink-0">
            {isCustomer ? <ShoppingBag className="w-5 h-5 text-[#B85C1A]" /> : <Ticket className="w-5 h-5 text-[#B85C1A]" />}
          </div>
          <div>
            <p className="text-sm font-semibold text-[#1A1817]">{isCustomer ? "My Tickets" : "My Events"}</p>
            <p className="text-xs text-[#8B8580]">
              {isCustomer ? `${bookings.length} booking${bookings.length !== 1 ? "s" : ""}` : `${myEvents.length} event${myEvents.length !== 1 ? "s" : ""}`}
            </p>
          </div>
        </div>
      </div>

      {/* Customer: Bookings list */}
      {isCustomer && (
        <>
          <p className="font-[family-name:var(--font-display)] text-xl text-[#1A1817] mb-5">My Tickets</p>

          {loading && (
            <div className="card text-center py-10">
              <Loader2 className="w-5 h-5 text-[#D4CEC4] animate-spin mx-auto mb-2" />
              <p className="text-[#8B8580] text-sm">Loading tickets...</p>
            </div>
          )}

          {!loading && bookings.length === 0 && (
            <div className="card text-center py-12">
              <ShoppingBag className="w-8 h-8 text-[#D4CEC4] mx-auto mb-3" />
              <p className="text-[#8B8580] text-sm mb-2">No bookings yet</p>
              <Link href="/" className="text-[#D9381E] text-sm font-medium hover:underline">
                Browse events &rarr;
              </Link>
            </div>
          )}

              {!loading && bookings.length > 0 && (
            <div className="space-y-3">
              {bookings.map(b => (
                <div key={b.id} className="card-stub p-4">
                  <div className="flex justify-between items-start">
                    <div>
                      <p className="text-sm font-semibold text-[#1A1817]">Booking {b.id.slice(0, 8)}&hellip;</p>
                      <p className="text-xs text-[#8B8580] mt-0.5">{fmtShortDate(b.created_at)}</p>
                    </div>
                    <div className="text-right">
                      <p className="font-semibold text-[#1A1817]">${(b.total_cents / 100).toFixed(2)}</p>
                      <span className={`badge text-[0.65rem] mt-1 ${
                        b.status === "confirmed" ? "badge-green" :
                        b.status === "cancelled" ? "badge-red" : "badge-yellow"
                      }`}>{b.status}</span>
                    </div>
                  </div>
                  {b.items.length > 0 && (
                    <div className="mt-3 pt-3 border-t border-[#E5E0D5] space-y-1.5">
                      {b.items.map((item, i) => (
                        <div key={i} className="flex justify-between text-xs">
                          <span className="text-[#8B8580]">{item.quantity} × ticket</span>
                          <span className="font-medium text-[#1A1817]">
                            ${((item.unit_price_cents * item.quantity) / 100).toFixed(2)}
                          </span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}

          {/* Payment History */}
          <p className="font-[family-name:var(--font-display)] text-xl text-[#1A1817] mb-5 mt-12">Payment History</p>

          {loading && (
            <div className="card text-center py-10">
              <Loader2 className="w-5 h-5 text-[#D4CEC4] animate-spin mx-auto mb-2" />
              <p className="text-[#8B8580] text-sm">Loading payments...</p>
            </div>
          )}

          {!loading && transactions.length === 0 && (
            <div className="card text-center py-12">
              <CreditCard className="w-8 h-8 text-[#D4CEC4] mx-auto mb-3" />
              <p className="text-[#8B8580] text-sm">No payment history yet</p>
            </div>
          )}

          {!loading && transactions.length > 0 && (
            <div className="space-y-3">
              {transactions.map(t => (
                <div key={t.id} className="card-stub flex justify-between items-center p-4">
                  <div>
                    <p className="text-sm font-semibold text-[#1A1817]">Txn {t.id.slice(0, 8)}&hellip;</p>
                    <p className="text-xs text-[#8B8580] mt-0.5">{fmtShortDate(t.created_at)}</p>
                  </div>
                  <div className="text-right">
                    <p className="font-semibold text-[#1A1817]">${(t.amount_cents / 100).toFixed(2)}</p>
                    <span className={`badge text-[0.65rem] mt-1 ${
                      t.status === "completed" ? "badge-green" :
                      t.status === "failed" ? "badge-red" :
                      t.status === "processing" ? "badge-yellow" : "badge-ink"
                    }`}>{t.status}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </>
      )}

      {/* EO/Admin: Own events list */}
      {(isEO || isAdmin) && (
        <>
          <p className="font-[family-name:var(--font-display)] text-xl text-[#1A1817] mb-5">My Events</p>

          {loading && (
            <div className="card text-center py-10">
              <Loader2 className="w-5 h-5 text-[#D4CEC4] animate-spin mx-auto mb-2" />
              <p className="text-[#8B8580] text-sm">Loading events...</p>
            </div>
          )}

          {!loading && myEvents.length === 0 && (
            <div className="card text-center py-12">
              <Ticket className="w-8 h-8 text-[#D4CEC4] mx-auto mb-3" />
              <p className="text-[#8B8580] text-sm mb-2">No events yet</p>
              <Link href="/events" className="text-[#D9381E] text-sm font-medium hover:underline">
                Create your first event &rarr;
              </Link>
            </div>
          )}

          {!loading && myEvents.length > 0 && (
            <div className="space-y-3">
              {myEvents.map(e => (
                <Link key={e.id} href={`/events/${e.id}`} className="card-stub flex justify-between items-center p-4 group cursor-pointer">
                  <div>
                    <p className="text-sm font-semibold text-[#1A1817] group-hover:text-[#D9381E] transition-colors">{e.title}</p>
                    <p className="text-xs text-[#8B8580] mt-0.5">
                      {fmtShortDate(e.start_at)} &middot; {e.venue_name}
                    </p>
                  </div>
                  <span className={`badge ${
                    e.status === "published" ? "badge-green" :
                    e.status === "pending" ? "badge-yellow" :
                    e.status === "rejected" || e.status === "cancelled" ? "badge-red" : "badge-ink"
                  }`}>{e.status}</span>
                </Link>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}

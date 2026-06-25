"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import Link from "next/link";
import { api } from "@/lib/api-client";
import { adminApi } from "@/lib/admin-api";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import {
  LayoutDashboard,
  CalendarPlus,
  Shield,
  ShoppingBag,
  Ticket,
  CreditCard,
  ChevronRight,
  Loader2,
  Check,
  X,
  Calendar,
  MapPin,
  Users,
} from "lucide-react";
import { fmtShortDate } from "@/lib/format";
import type { EventItem, TransactionResponse } from "@/types";

type AdminFilter = "" | "pending" | "published" | "cancelled";

const FILTER_LABELS: { key: AdminFilter; label: string }[] = [
  { key: "", label: "All" },
  { key: "pending", label: "Pending" },
  { key: "published", label: "Published" },
  { key: "cancelled", label: "Cancelled" },
];

interface BookingItem {
  ticket_type_id: string;
  quantity: number;
  unit_price_cents: number;
}

interface Booking {
  id: string;
  event_id: string;
  status: string;
  refund_status?: string;
  total_cents: number;
  created_at: string;
  items: BookingItem[];
}

export default function DashboardPage() {
  const { user, hydrated, isAdmin, isEO, isCustomer } = useAuth();
  const queryClient = useQueryClient();

  const [adminFilter, setAdminFilter] = useState<AdminFilter>("pending");
  const [rejecting, setRejecting] = useState<EventItem | null>(null);
  const [cancelling, setCancelling] = useState<EventItem | null>(null);
  const [approving, setApproving] = useState<EventItem | null>(null);

  // Customer queries
  const bookingsQuery = useQuery({
    queryKey: ["bookings"],
    queryFn: () => api.get<Booking[]>("/api/bookings"),
    enabled: !!user && isCustomer,
  });

  const transactionsQuery = useQuery({
    queryKey: ["transactions"],
    queryFn: () => api.get<TransactionResponse[]>("/api/payments"),
    enabled: !!user && isCustomer,
  });

  // EO query
  const myEventsQuery = useQuery({
    queryKey: ["myEvents"],
    queryFn: () => api.get<EventItem[]>("/api/events/mine"),
    enabled: !!user && isEO,
  });

  // Admin query
  const adminEventsQuery = useQuery({
    queryKey: ["adminEvents", adminFilter],
    queryFn: () => adminApi.listAll(adminFilter, 50),
    enabled: !!user && isAdmin,
  });

  if (!hydrated) return null;
  if (!user) return null;

  const bookings = bookingsQuery.data || [];
  const transactions = transactionsQuery.data || [];
  const myEvents = myEventsQuery.data || [];
  const adminEvents = adminEventsQuery.data?.events || [];
  const adminTotal = adminEventsQuery.data?.total || 0;

  const eoLoading = myEventsQuery.isLoading;
  const adminLoading = adminEventsQuery.isLoading;

  const badgeClass = (s: string) =>
    s === "published" ? "badge-green" :
    s === "cancelled" || s === "rejected" ? "badge-red" :
    s === "pending" ? "badge-yellow" : "badge-ink";

  return (
    <div>
      {/* Header */}
      <div className="mb-10">
        <p className="font-[family-name:var(--font-display)] text-3xl text-[#1A1817] mb-1">
          {isAdmin ? "Admin Dashboard" : "Dashboard"}
        </p>
        <p className="text-[#8B8580] text-sm flex items-center gap-2">
          {user.name}
          <span className={`badge ${
            isAdmin ? "badge-blue" : isEO ? "badge-green" : "badge-ink"
          }`}>{user.role}</span>
        </p>
      </div>

      {/* ── ADMIN ── */}
      {isAdmin && (
        <>
          {/* Stats */}
          <div className="grid gap-4 sm:grid-cols-3 mb-8 fade-up">
            <div className="card flex items-center gap-4 p-4">
              <div className="w-10 h-10 rounded-lg bg-[#1A5DB8]/6 flex items-center justify-center flex-shrink-0">
                <Shield className="w-5 h-5 text-[#1A5DB8]" />
              </div>
              <div>
                <p className="text-2xl font-semibold text-[#1A1817]">{adminTotal}</p>
                <p className="text-xs text-[#8B8580]">Total Events</p>
              </div>
            </div>
            <Link href="/dashboard" onClick={() => setAdminFilter("pending")} className="card flex items-center gap-4 p-4 group cursor-pointer">
              <div className="w-10 h-10 rounded-lg bg-[#B85C1A]/6 flex items-center justify-center flex-shrink-0">
                <Calendar className="w-5 h-5 text-[#B85C1A]" />
              </div>
              <div>
                <p className="text-2xl font-semibold text-[#1A1817]">
                  {adminFilter === "pending" ? adminTotal : "..."}
                </p>
                <p className="text-xs text-[#8B8580]">Pending Review</p>
              </div>
            </Link>
            <div className="card flex items-center gap-4 p-4">
              <div className="w-10 h-10 rounded-lg bg-[#2D7A46]/6 flex items-center justify-center flex-shrink-0">
                <Check className="w-5 h-5 text-[#2D7A46]" />
              </div>
              <div>
                <p className="text-lg font-semibold text-[#1A1817]">
                  {adminFilter ? adminFilter : "all"}
                </p>
                <p className="text-xs text-[#8B8580]">Current Filter</p>
              </div>
            </div>
          </div>

          {/* Filter tabs */}
          <div className="flex gap-2 mb-6 overflow-x-auto pb-2">
            {FILTER_LABELS.map(f => (
              <button
                key={f.key}
                onClick={() => setAdminFilter(f.key)}
                className={`text-sm font-medium px-4 py-1.5 rounded-md transition-colors duration-150 whitespace-nowrap ${
                  adminFilter === f.key
                    ? "bg-[#D9381E] text-white"
                    : "text-[#4A4541] hover:bg-[#F5F0E8]"
                }`}
              >
                {f.label}
              </button>
            ))}
          </div>

          {/* Loading */}
          {adminLoading && (
            <div className="card text-center py-10">
              <Loader2 className="w-5 h-5 text-[#D4CEC4] animate-spin mx-auto mb-2" />
              <p className="text-[#8B8580] text-sm">Loading events...</p>
            </div>
          )}

          {/* Empty */}
          {!adminLoading && adminEvents.length === 0 && (
            <div className="card text-center py-12">
              <Ticket className="w-8 h-8 text-[#D4CEC4] mx-auto mb-3" />
              <p className="text-[#8B8580] text-sm">No events found for this filter</p>
            </div>
          )}

          {/* Event cards */}
          {!adminLoading && adminEvents.length > 0 && (
            <div className="space-y-4">
              {adminEvents.map(event => (
                <div key={event.id} className="card-stub space-y-3">
                  <div className="flex items-start justify-between">
                    <div>
                      <h3 className="font-[family-name:var(--font-display)] text-lg text-[#1A1817]">{event.title}</h3>
                      <p className="text-sm text-[#8B8580] mt-1 line-clamp-1">{event.description || "No description"}</p>
                    </div>
                    <span className={badgeClass(event.status)}>{event.status}</span>
                  </div>
                  <div className="flex flex-wrap gap-4 text-xs text-[#8B8580]">
                    {event.venue_name ? (
                      <span className="flex items-center gap-1.5"><MapPin className="w-3.5 h-3.5 text-[#B0A89E]" />{event.venue_name}</span>
                    ) : null}
                    {event.venue_capacity > 0 && (
                      <span className="flex items-center gap-1.5"><Users className="w-3.5 h-3.5 text-[#B0A89E]" />{event.venue_capacity}</span>
                    )}
                    <span className="flex items-center gap-1.5"><Calendar className="w-3.5 h-3.5 text-[#B0A89E]" />{fmtShortDate(event.start_at)}</span>
                  </div>
                  <div className="flex gap-2 pt-2 border-t border-dashed border-[#E8E3DC]">
                    <Link href={`/events/${event.id}`} className="btn-outline text-sm">View</Link>
                    {event.status === "pending" && (
                      <>
                        <button onClick={() => setApproving(event)} className="btn-approve text-sm">
                          <Check className="w-4 h-4" />Approve
                        </button>
                        <button onClick={() => setRejecting(event)} className="btn-danger text-sm"><X className="w-4 h-4" />Reject</button>
                      </>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </>
      )}

      {/* ── EO ── */}
      {isEO && (
        <>
          <div className="grid gap-4 sm:grid-cols-2 mb-12 fade-up">
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
            <div className="card flex items-center gap-4 p-4">
              <div className="w-10 h-10 rounded-lg bg-[#B85C1A]/6 flex items-center justify-center flex-shrink-0">
                <Ticket className="w-5 h-5 text-[#B85C1A]" />
              </div>
              <div>
                <p className="text-sm font-semibold text-[#1A1817]">My Events</p>
                <p className="text-xs text-[#8B8580]">
                  {myEvents.length} event{myEvents.length !== 1 ? "s" : ""}
                </p>
              </div>
            </div>
          </div>

          <p className="font-[family-name:var(--font-display)] text-xl text-[#1A1817] mb-5">My Events</p>

          {eoLoading && (
            <div className="card text-center py-10">
              <Loader2 className="w-5 h-5 text-[#D4CEC4] animate-spin mx-auto mb-2" />
              <p className="text-[#8B8580] text-sm">Loading events...</p>
            </div>
          )}

          {!eoLoading && myEvents.length === 0 && (
            <div className="card text-center py-12">
              <Ticket className="w-8 h-8 text-[#D4CEC4] mx-auto mb-3" />
              <p className="text-[#8B8580] text-sm mb-2">No events yet</p>
              <Link href="/events" className="text-[#D9381E] text-sm font-medium hover:underline">
                Create your first event &rarr;
              </Link>
            </div>
          )}

          {!eoLoading && myEvents.length > 0 && (
            <div className="space-y-3">
              {myEvents.map(e => (
                <div key={e.id} className="card-stub flex justify-between items-center p-4">
                  <Link href={`/events/${e.id}`} className="group flex-1 min-w-0">
                    <p className="text-sm font-semibold text-[#1A1817] group-hover:text-[#D9381E] transition-colors">{e.title}</p>
                    <p className="text-xs text-[#8B8580] mt-0.5">
                      {fmtShortDate(e.start_at)} &middot; {e.venue_name}
                    </p>
                  </Link>
                  <div className="flex items-center gap-2 ml-4 flex-shrink-0">
                    <span className={badgeClass(e.status)}>{e.status}</span>
                    {e.status !== "cancelled" && (
                      <button
                        onClick={() => setCancelling(e)}
                        className="flex items-center gap-1 text-xs font-medium px-2 py-1 rounded-md text-[#D9381E] hover:bg-[#FFF5F5] transition-colors duration-150"
                      >
                        <X className="w-3.5 h-3.5" />
                        Cancel
                      </button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </>
      )}

      {/* ── CUSTOMER ── */}
      {isCustomer && (
        <>
          <div className="grid gap-4 sm:grid-cols-2 mb-12 fade-up">
            <div className="card flex items-center gap-4 p-4">
              <div className="w-10 h-10 rounded-lg bg-[#B85C1A]/6 flex items-center justify-center flex-shrink-0">
                <ShoppingBag className="w-5 h-5 text-[#B85C1A]" />
              </div>
              <div>
                <p className="text-sm font-semibold text-[#1A1817]">My Tickets</p>
                <p className="text-xs text-[#8B8580]">
                  {bookings.length} booking{bookings.length !== 1 ? "s" : ""}
                </p>
              </div>
            </div>
            <div className="card flex items-center gap-4 p-4">
              <div className="w-10 h-10 rounded-lg bg-[#1A5DB8]/6 flex items-center justify-center flex-shrink-0">
                <CreditCard className="w-5 h-5 text-[#1A5DB8]" />
              </div>
              <div>
                <p className="text-sm font-semibold text-[#1A1817]">Payments</p>
                <p className="text-xs text-[#8B8580]">
                  {transactions.length} transaction{transactions.length !== 1 ? "s" : ""}
                </p>
              </div>
            </div>
          </div>

          {/* Bookings */}
          <p className="font-[family-name:var(--font-display)] text-xl text-[#1A1817] mb-5">My Tickets</p>

          {bookingsQuery.isLoading && (
            <div className="card text-center py-10">
              <Loader2 className="w-5 h-5 text-[#D4CEC4] animate-spin mx-auto mb-2" />
              <p className="text-[#8B8580] text-sm">Loading tickets...</p>
            </div>
          )}

          {!bookingsQuery.isLoading && bookings.length === 0 && (
            <div className="card text-center py-12">
              <ShoppingBag className="w-8 h-8 text-[#D4CEC4] mx-auto mb-3" />
              <p className="text-[#8B8580] text-sm mb-2">No bookings yet</p>
              <Link href="/" className="text-[#D9381E] text-sm font-medium hover:underline">
                Browse events &rarr;
              </Link>
            </div>
          )}

          {!bookingsQuery.isLoading && bookings.length > 0 && (
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
                        b.status === "confirmed" ? "badge-green" : "badge-red"
                      }`}>{b.status}</span>
                    </div>
                  </div>
                  {b.items.length > 0 && (
                    <div className="mt-3 pt-3 border-t border-[#E5E0D5] space-y-1.5">
                      {b.items.map((item, i) => (
                        <div key={i} className="flex justify-between text-xs">
                          <span className="text-[#8B8580]">{item.quantity} &times; ticket</span>
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

          {transactionsQuery.isLoading && (
            <div className="card text-center py-10">
              <Loader2 className="w-5 h-5 text-[#D4CEC4] animate-spin mx-auto mb-2" />
              <p className="text-[#8B8580] text-sm">Loading payments...</p>
            </div>
          )}

          {!transactionsQuery.isLoading && transactions.length === 0 && (
            <div className="card text-center py-12">
              <CreditCard className="w-8 h-8 text-[#D4CEC4] mx-auto mb-3" />
              <p className="text-[#8B8580] text-sm">No payment history yet</p>
            </div>
          )}

          {!transactionsQuery.isLoading && transactions.length > 0 && (
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

      {/* Cancel event dialog */}
      <ConfirmDialog
        open={!!cancelling}
        title="Cancel Event"
        message={`Are you sure you want to cancel "${cancelling?.title}"? This will cancel all bookings and create refunds.`}
        confirmLabel="Yes, Cancel"
        variant="danger"
        loading={false}
        onConfirm={() => {
          if (!cancelling) return;
          api.post<void>(`/api/events/${cancelling.id}/cancel`)
            .then(() => {
              toast.success("Event cancelled");
              queryClient.invalidateQueries({ queryKey: ["myEvents"] });
            })
            .catch((err: Error) => toast.error(err.message))
            .finally(() => setCancelling(null));
        }}
        onClose={() => setCancelling(null)}
      />

      {/* Approve event dialog */}
      <ConfirmDialog
        open={!!approving}
        title="Approve Event"
        message={`Approve "${approving?.title}" and make it public?`}
        confirmLabel="Yes, Approve"
        variant="success"
        loading={false}
        onConfirm={() => {
          if (!approving) return;
          adminApi.approveEvent(approving.id)
            .then(() => {
              toast.success("Event approved");
              queryClient.invalidateQueries({ queryKey: ["adminEvents"] });
            })
            .catch((err: Error) => toast.error(err.message))
            .finally(() => setApproving(null));
        }}
        onClose={() => setApproving(null)}
      />

      {/* Reject event dialog */}
      <ConfirmDialog
        open={!!rejecting}
        title="Reject Event"
        message={`Reject "${rejecting?.title}" and notify the organizer?`}
        confirmLabel="Reject"
        variant="danger"
        requireReason
        onConfirm={(reason) => {
          if (!rejecting) return;
          adminApi.rejectEvent(rejecting.id, reason || "")
            .then(() => {
              toast.success("Event rejected");
              queryClient.invalidateQueries({ queryKey: ["adminEvents"] });
            })
            .catch((err: Error) => toast.error(err.message))
            .finally(() => setRejecting(null));
        }}
        onClose={() => setRejecting(null)}
      />
    </div>
  );
}

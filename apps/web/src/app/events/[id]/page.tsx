"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { api } from "@/lib/api-client";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import { Calendar, MapPin, Users, Clock, Ticket as TicketIcon, ArrowRight, ChevronLeft, Loader2 } from "lucide-react";
import { fmtDateTime, fmtTime } from "@/lib/format";
import type { EventDetail, ReservationResponse } from "@/types";

type Phase = "selection" | "summary";

export default function EventPage() {
  const { id } = useParams<{ id: string }>();
  const [event, setEvent] = useState<EventDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [phase, setPhase] = useState<Phase>("selection");
  const [submitting, setSubmitting] = useState(false);
  const [selectedTickets, setSelectedTickets] = useState<Record<string, number>>({});
  const { user, hydrated, isEO, isAdmin } = useAuth();
  const router = useRouter();

  useEffect(() => {
    api.get<EventDetail>(`/api/events/${id}`)
      .then(setEvent)
      .catch(() => setEvent(null))
      .finally(() => setLoading(false));
  }, [id]);

  const hasSelection = Object.values(selectedTickets).some(q => q > 0);

  function getOrderItems() {
    return Object.entries(selectedTickets)
      .filter(([, qty]) => qty > 0)
      .map(([ticketTypeId, qty]) => {
        const tt = event?.ticket_types.find(t => t.id === ticketTypeId);
        return {
          ticket_type_id: ticketTypeId,
          name: tt?.name || "",
          quantity: qty,
          unit_price_cents: tt?.price_cents || 0,
        };
      });
  }

  const orderItems = getOrderItems();
  const orderTotal = orderItems.reduce((sum, item) => sum + item.unit_price_cents * item.quantity, 0);

  function goToSummary() {
    if (!hasSelection) return;
    setPhase("summary");
  }

  function goToSelection() {
    setPhase("selection");
  }

  async function handleCheckout() {
    if (!user) { router.push("/login"); return; }
    setSubmitting(true);
    try {
      const items = orderItems.map(i => ({
        ticket_type_id: i.ticket_type_id,
        quantity: i.quantity,
        unit_price_cents: i.unit_price_cents,
      }));
      const res = await api.post<ReservationResponse>("/api/inventory/reserve", {
        event_id: id,
        items,
      });
      router.push("/checkout?booking_id=" + res.booking_id);
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : "Failed to reserve tickets");
      setSubmitting(false);
    }
  }

  if (loading) {
    return (
      <div className="max-w-2xl mx-auto space-y-5">
        <div className="skeleton h-10 w-3/4" />
        <div className="skeleton h-5 w-1/2" />
        <div className="skeleton h-40 w-full" />
        <div className="space-y-3">{Array.from({ length: 3 }).map((_, i) => <div key={i} className="skeleton h-20" />)}</div>
      </div>
    );
  }

  if (!event) {
    return (
      <div className="card text-center py-16 max-w-md mx-auto">
        <TicketIcon className="w-8 h-8 text-[#D4CEC4] mx-auto mb-3" />
        <p className="text-[#8B8580] text-sm">Event not found</p>
      </div>
    );
  }

  return (
    <div className="max-w-2xl mx-auto">
      {/* Hero */}
      <div className="card-stub mb-6">
        <span className={`badge mb-3 ${event.event.status === "published" ? "badge-green" : "badge-yellow"}`}>
          {event.event.status}
        </span>
        <h1 className="font-[family-name:var(--font-display)] text-2xl text-[#1A1817] mb-3">{event.event.title}</h1>
        <p className="text-[#8B8580] text-sm mb-4">{event.event.description}</p>
        <div className="grid grid-cols-2 gap-3 text-sm">
          <div className="flex items-center gap-2 text-[#4A4541]">
            <Calendar className="w-4 h-4 text-[#B0A89E]" />
            <span>{fmtDateTime(event.event.start_at)}</span>
          </div>
          <div className="flex items-center gap-2 text-[#4A4541]">
            <Clock className="w-4 h-4 text-[#B0A89E]" />
            <span>{fmtTime(event.event.start_at)} &mdash; {fmtTime(event.event.end_at)}</span>
          </div>
          <div className="flex items-center gap-2 text-[#4A4541]">
            <MapPin className="w-4 h-4 text-[#B0A89E]" />
            <span>{event.event.venue_name}{event.event.venue_address ? `, ${event.event.venue_address}` : ""}</span>
          </div>
          <div className="flex items-center gap-2 text-[#4A4541]">
            <Users className="w-4 h-4 text-[#B0A89E]" />
            <span>Capacity: {event.event.venue_capacity}</span>
          </div>
        </div>
      </div>

      {hydrated && (isEO || isAdmin) && (
        <div className="card text-center py-6">
          <p className="text-sm text-[#8B8580]">Organizers cannot purchase tickets.</p>
        </div>
      )}

      {/* Phase: Selection */}
      {(!hydrated || (!isEO && !isAdmin)) && phase === "selection" && (
        <>
          <div className="card">
            <h2 className="font-[family-name:var(--font-display)] text-lg text-[#1A1817] mb-4">Tickets</h2>
            <div className="space-y-3">
              {event.ticket_types.map(tt => (
                <div key={tt.id} className="flex items-center justify-between border border-dashed border-[#E8E3DC] rounded-md p-3">
                  <div>
                    <p className="text-sm font-semibold text-[#1A1817]">{tt.name}</p>
                    <p className="text-xs text-[#8B8580]">${(tt.price_cents / 100).toFixed(2)} &middot; {tt.available} of {tt.quantity} available</p>
                  </div>
                  <select
                    value={selectedTickets[tt.id] || 0}
                    onChange={e => setSelectedTickets(p => ({ ...p, [tt.id]: parseInt(e.target.value) || 0 }))}
                    className="input-field w-auto text-sm"
                  >
                    {Array.from({ length: Math.min(tt.max_per_order, tt.available) + 1 }, (_, i) => (
                      <option key={i} value={i}>{i}</option>
                    ))}
                  </select>
                </div>
              ))}
            </div>
          </div>
          <button
            onClick={goToSummary}
            disabled={!hasSelection || !(hydrated && user)}
            className="btn-accent w-full mt-6 py-3 text-base"
          >
            {hydrated && user ? "Review Order" : "Login to Reserve"}
          </button>
        </>
      )}

      {/* Phase: Summary */}
      {phase === "summary" && (
        <>
          <div className="card space-y-4">
            <h2 className="font-[family-name:var(--font-display)] text-lg text-[#1A1817]">Order Summary</h2>
            <div className="space-y-2">
              {orderItems.map(item => (
                <div key={item.ticket_type_id} className="flex justify-between text-sm">
                  <span className="text-[#4A4541]">
                    {item.name} <span className="text-[#B0A89E]">{item.quantity} &times; ${(item.unit_price_cents / 100).toFixed(2)}</span>
                  </span>
                  <span className="font-medium text-[#1A1817]">${((item.unit_price_cents * item.quantity) / 100).toFixed(2)}</span>
                </div>
              ))}
            </div>
            <div className="border-t border-dashed border-[#E8E3DC] pt-3 flex justify-between items-center">
              <span className="font-semibold text-[#1A1817]">Total</span>
              <span className="font-[family-name:var(--font-display)] text-xl text-[#1A1817]">${(orderTotal / 100).toFixed(2)}</span>
            </div>
          </div>
          <div className="flex gap-3 mt-6">
            <button onClick={goToSelection} className="btn-outline flex-1">
              <ChevronLeft className="w-4 h-4" /> Back
            </button>
            <button onClick={handleCheckout} disabled={submitting} className="btn-accent flex-1">
              {submitting ? <Loader2 className="w-4 h-4 animate-spin mx-auto" /> : (
                <span className="flex items-center justify-center gap-2">Checkout <ArrowRight className="w-4 h-4" /></span>
              )}
            </button>
          </div>
        </>
      )}
    </div>
  );
}

"use client";

import { useQuery, useMutation } from "@tanstack/react-query";
import { useState } from "react";
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
  const { user, hydrated, isEO, isAdmin } = useAuth();
  const router = useRouter();

  const [phase, setPhase] = useState<Phase>("selection");
  const [selectedTickets, setSelectedTickets] = useState<Record<string, number>>({});

  const eventQuery = useQuery({
    queryKey: ["event", id],
    queryFn: () => api.get<EventDetail>(`/api/events/${id}`),
    staleTime: 10 * 1000,
  });

  const reserveMutation = useMutation({
    mutationFn: (items: { ticket_type_id: string; quantity: number; unit_price_cents: number }[]) =>
      api.post<ReservationResponse>("/api/inventory/reserve", { event_id: id, items }),
    onSuccess: (data) => {
      router.push("/checkout?booking_id=" + data.booking_id);
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });

  const event = eventQuery.data;
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

  function handleCheckout() {
    if (!user) { router.push("/login"); return; }
    reserveMutation.mutate(orderItems.map(i => ({
      ticket_type_id: i.ticket_type_id,
      quantity: i.quantity,
      unit_price_cents: i.unit_price_cents,
    })));
  }

  if (eventQuery.isLoading) {
    return (
      <div className="max-w-2xl mx-auto space-y-5">
        <div className="skeleton h-10 w-3/4" />
        <div className="skeleton h-5 w-1/2" />
        <div className="skeleton h-40 w-full" />
        <div className="space-y-3">{Array.from({ length: 3 }).map((_, i) => <div key={i} className="skeleton h-20" />)}</div>
      </div>
    );
  }

  if (eventQuery.isError || !event) {
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

        {/* Ticket summary strip */}
        <div className="border-t border-dashed border-[#E8E3DC] pt-4 mt-3">
          <p className="text-xs text-[#B0A89E] mb-3 font-medium uppercase tracking-wider">
            Tickets Available
          </p>
          <div className="flex flex-wrap gap-2">
            {event.ticket_types.map(tt => {
              const soldOut = tt.available === 0;
              return (
                <div key={tt.id}
                  className={`rounded-md px-3 py-2 min-w-[100px] ${
                    soldOut ? "bg-[#F5F2EC] opacity-60" : "bg-[#FEFBF6] border border-[#E8E3DC]"
                  }`}
                >
                  <p className="text-xs font-semibold text-[#1A1817]">{tt.name}</p>
                  <p className="text-xs text-[#D9381E] font-medium">${(tt.price_cents / 100).toFixed(2)}</p>
                  <p className="text-[0.6rem] text-[#B0A89E]">
                    {soldOut ? "Sold out" : `${tt.available} left`}
                  </p>
                </div>
              );
            })}
          </div>
        </div>
      </div>

      {(isEO || isAdmin) && (
        <div className="card text-center py-6">
          <p className="text-sm text-[#8B8580]">Organizers cannot purchase tickets.</p>
        </div>
      )}

      {/* Phase: Selection */}
      {(!isEO && !isAdmin) && phase === "selection" && (
        <>
          <div className="card">
            <h2 className="font-[family-name:var(--font-display)] text-lg text-[#1A1817] mb-4">Tickets</h2>
            <div className="space-y-3">
              {event.ticket_types.map(tt => {
                const soldCount = tt.quantity - tt.available;
                const soldPercent = tt.quantity > 0 ? Math.round((soldCount / tt.quantity) * 100) : 0;
                const isLowStock = tt.available > 0 && tt.available < 5;
                const isSoldOut = tt.available === 0;

                return (
                  <div key={tt.id} className="border border-dashed border-[#E8E3DC] rounded-md p-4 space-y-3">
                    <div className="flex items-start justify-between">
                      <div>
                        <p className="text-sm font-semibold text-[#1A1817]">{tt.name}</p>
                        <p className="font-[family-name:var(--font-display)] text-xl text-[#1A1817] mt-0.5">
                          ${(tt.price_cents / 100).toFixed(2)}
                        </p>
                      </div>
                      <div className="flex items-center gap-2">
                        {isLowStock && (
                          <span className="badge badge-red text-[0.6rem]">Only {tt.available} left!</span>
                        )}
                        {isSoldOut && (
                          <span className="badge badge-ink text-[0.6rem]">Sold out</span>
                        )}
                        <select
                          value={selectedTickets[tt.id] || 0}
                          onChange={e => setSelectedTickets(p => ({ ...p, [tt.id]: parseInt(e.target.value) || 0 }))}
                          disabled={isSoldOut}
                          className="input-field w-auto text-sm"
                        >
                          {Array.from({ length: Math.min(tt.max_per_order, tt.available) + 1 }, (_, i) => (
                            <option key={i} value={i}>{i}</option>
                          ))}
                        </select>
                      </div>
                    </div>

                    {/* Availability bar */}
                    <div className="space-y-1">
                      <div className="flex justify-between text-[0.65rem] text-[#B0A89E]">
                        <span>{soldPercent}% sold</span>
                        <span>{tt.available} of {tt.quantity} left</span>
                      </div>
                      <div className="w-full h-1.5 rounded-full bg-[#F0EDE6] overflow-hidden">
                        <div
                          className="h-full rounded-full bg-[#D9381E] transition-all"
                          style={{ width: `${soldPercent}%` }}
                        />
                      </div>
                    </div>

                    {/* Max per order */}
                    <p className="text-[0.65rem] text-[#B0A89E]">
                      Max {tt.max_per_order} per order
                    </p>
                  </div>
                );
              })}
            </div>
          </div>
          <button
            onClick={() => setPhase("summary")}
            disabled={!hasSelection || !hydrated || !user}
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
            <button onClick={() => setPhase("selection")} className="btn-outline flex-1">
              <ChevronLeft className="w-4 h-4" /> Back
            </button>
            <button
              onClick={handleCheckout}
              disabled={reserveMutation.isPending}
              className="btn-accent flex-1"
            >
              {reserveMutation.isPending ? <Loader2 className="w-4 h-4 animate-spin mx-auto" /> : (
                <span className="flex items-center justify-center gap-2">Checkout <ArrowRight className="w-4 h-4" /></span>
              )}
            </button>
          </div>
        </>
      )}
    </div>
  );
}

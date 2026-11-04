"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { api } from "@/lib/api-client";
import { adminApi } from "@/lib/admin-api";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import {
  Calendar,
  MapPin,
  Users,
  Timer,
  Ticket as TicketIcon,
  ArrowRight,
  ChevronLeft,
  Loader2,
  X,
  Minus,
  Plus,
  Check,
  ShieldCheck,
} from "lucide-react";
import { fmtIDR } from "@/lib/format";
import { LocalTime } from "@/components/ui/local-time";
import type { EventDetail, ReservationResponse } from "@/types";

type Phase = "selection" | "summary";

function Stepper({
  value,
  max,
  disabled,
  onChange,
}: {
  value: number;
  max: number;
  disabled?: boolean;
  onChange: (v: number) => void;
}) {
  return (
    <div className="flex items-center gap-2.5">
      <button
        type="button"
        aria-label="Kurangi"
        onClick={() => onChange(Math.max(0, value - 1))}
        disabled={disabled || value <= 0}
        className="w-7 h-7 rounded-full border border-[#E4EAF4] bg-white flex items-center justify-center text-[#6E7B96] hover:border-[#C9D4E8] disabled:opacity-40 transition-colors"
      >
        <Minus className="w-3.5 h-3.5" />
      </button>
      <span className="w-5 text-center text-sm font-bold text-[#16224A]">
        {value}
      </span>
      <button
        type="button"
        aria-label="Tambah"
        onClick={() => onChange(Math.min(max, value + 1))}
        disabled={disabled || value >= max}
        className="w-7 h-7 rounded-full bg-[#2450D9] text-white flex items-center justify-center hover:bg-[#1D3FAE] disabled:opacity-40 transition-colors"
      >
        <Plus className="w-3.5 h-3.5" />
      </button>
    </div>
  );
}

export default function EventPage() {
  const { id } = useParams<{ id: string }>();
  const { user, hydrated, isEO, isAdmin } = useAuth();
  const router = useRouter();
  const queryClient = useQueryClient();

  const [phase, setPhase] = useState<Phase>("selection");
  const [confirming, setConfirming] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [approving, setApproving] = useState(false);
  const [rejecting, setRejecting] = useState(false);
  const [selectedTickets, setSelectedTickets] = useState<
    Record<string, number>
  >({});

  const eventQuery = useQuery({
    queryKey: ["event", id],
    queryFn: () => api.get<EventDetail>(`/api/events/${id}`),
    staleTime: 10 * 1000,
  });

  const reserveMutation = useMutation({
    mutationFn: (
      items: {
        ticket_type_id: string;
        quantity: number;
        unit_price_rupiah: number;
      }[],
    ) =>
      api.post<ReservationResponse>("/api/bookings/reserve", {
        event_id: id,
        items,
      }),
    onSuccess: (data) => {
      router.push("/checkout/" + data.booking_id);
    },
    onError: (err: Error) => {
      toast.error(err.message);
    },
  });

  const event = eventQuery.data;
  const hasSelection = Object.values(selectedTickets).some((q) => q > 0);
  // The server enforces the reservation window; this only drives the UI.
  const reservationsOpen = event?.event.reservations_open !== false;
  const onSaleNow = event?.event.status === "published";

  function getOrderItems() {
    return Object.entries(selectedTickets)
      .filter(([, qty]) => qty > 0)
      .map(([ticketTypeId, qty]) => {
        const tt = event?.ticket_types.find((t) => t.id === ticketTypeId);
        return {
          ticket_type_id: ticketTypeId,
          name: tt?.name || "",
          quantity: qty,
          unit_price_rupiah: tt?.price_rupiah || 0,
        };
      });
  }

  const orderItems = getOrderItems();
  const orderTotal = orderItems.reduce(
    (sum, item) => sum + item.unit_price_rupiah * item.quantity,
    0,
  );

  function handleCheckout() {
    if (!user) {
      router.push("/login");
      return;
    }
    reserveMutation.mutate(
      orderItems.map((i) => ({
        ticket_type_id: i.ticket_type_id,
        quantity: i.quantity,
        unit_price_rupiah: i.unit_price_rupiah,
      })),
    );
  }
  if (eventQuery.isLoading) {
    return (
      <div className="max-w-5xl mx-auto space-y-5">
        <div className="skeleton h-56 w-full" />
        <div className="skeleton h-10 w-3/4" />
        <div className="space-y-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="skeleton h-20" />
          ))}
        </div>
      </div>
    );
  }

  if (eventQuery.isError || !event) {
    return (
      <div className="card text-center py-16 max-w-md mx-auto">
        <TicketIcon className="w-8 h-8 text-[#C9D4E8] mx-auto mb-3" />
        <p className="text-[#6E7B96] text-sm">Event not found</p>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto">
      {/* Banner */}
      <div className="relative h-[220px] md:h-[260px] w-full rounded-[20px] overflow-hidden bg-[#F0F5FF] mb-6">
        {event.event.image_url ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={event.event.image_url}
            alt={event.event.title}
            className="absolute inset-0 w-full h-full object-cover"
          />
        ) : (
          <span className="absolute inset-0 flex items-center justify-center font-[family-name:var(--font-display)] text-5xl text-[#C9D4E8]">
            {event.event.title
              .split(/\s+/)
              .slice(0, 2)
              .map((w) => w[0]?.toUpperCase())
              .join("")}
          </span>
        )}
        <span
          className={`badge absolute top-3.5 right-3.5 ${
            event.event.status === "published" ? "badge-green" : "badge-ink"
          }`}
        >
          {event.event.status}
        </span>
        <span className="glass-chip absolute bottom-3.5 left-3.5">
          <LocalTime iso={event.event.start_at} format="datetime" />
        </span>
        {event.event.venue_name ? (
          <span className="glass-chip absolute bottom-3.5 left-auto right-3.5 max-w-[45%]">
            <span className="truncate">{event.event.venue_name}</span>
          </span>
        ) : null}
      </div>

      <div className="grid lg:grid-cols-[1fr_380px] gap-8 items-start">
        {/* Left: title, meta, description */}
        <div>
          <h1 className="font-[family-name:var(--font-display)] text-4xl text-[#16224A] leading-tight mb-4">
            {event.event.title}
          </h1>
          <div className="flex flex-wrap gap-x-5 gap-y-2 mb-5">
            <span className="flex items-center gap-2 text-sm font-semibold text-[#16224A]">
              <Calendar className="w-4 h-4 text-[#2450D9]" />
              <LocalTime iso={event.event.start_at} format="datetime" />
            </span>
            <span className="flex items-center gap-2 text-sm font-semibold text-[#16224A]">
              <Timer className="w-4 h-4 text-[#2450D9]" />
              <LocalTime iso={event.event.start_at} format="time" /> &mdash;{" "}
              <LocalTime iso={event.event.end_at} format="time" />
            </span>
            <span className="flex items-center gap-2 text-sm font-semibold text-[#16224A]">
              <MapPin className="w-4 h-4 text-[#2450D9]" />
              {event.event.venue_name}
            </span>
            <span className="flex items-center gap-2 text-sm font-semibold text-[#16224A]">
              <Users className="w-4 h-4 text-[#2450D9]" />
              Capacity {event.event.venue_capacity}
            </span>
          </div>
          {event.event.description ? (
            <p className="text-[15px] leading-relaxed text-[#6E7B96] mb-6">
              {event.event.description}
            </p>
          ) : null}

          {(isEO || isAdmin) && (
            <div className="card text-center py-6">
              {isAdmin && event?.event.status === "pending" ? (
                <>
                  <p className="text-sm font-bold text-[#16224A] mb-1">
                    Menunggu persetujuan
                  </p>
                  <p className="text-sm text-[#6E7B96] mb-4">
                    Event ini belum tayang. Setujui untuk membuatnya publik.
                  </p>
                  <div className="flex gap-3 justify-center">
                    <button
                      onClick={() => setRejecting(true)}
                      className="btn-outline text-sm"
                    >
                      <X className="w-4 h-4" /> Tolak
                    </button>
                    <button
                      onClick={() => setApproving(true)}
                      className="btn-approve text-sm"
                    >
                      <Check className="w-4 h-4" /> Setujui
                    </button>
                  </div>
                </>
              ) : (
                <>
                  <p className="text-sm text-[#6E7B96] mb-4">
                    Organizers cannot purchase tickets.
                  </p>
                  {isEO && event?.event.status === "published" && (
                    <button
                      onClick={() => setCancelling(true)}
                      className="btn-ghost text-xs font-bold text-[#16224A] mx-auto"
                    >
                      <X className="w-3.5 h-3.5" />
                      Cancel Event
                    </button>
                  )}
                </>
              )}
            </div>
          )}
        </div>

        {/* Right: booking panel */}
        <div className="card p-5 flex flex-col gap-4">
          <div className="flex items-center justify-between">
            <h2 className="font-[family-name:var(--font-display)] text-[22px] text-[#16224A]">
              {phase === "selection" ? "Tiket" : "Order Summary"}
            </h2>
            <span
              className={`badge ${
                onSaleNow ? "badge-blue" : "badge-ink"
              }`}
            >
              {onSaleNow ? "On sale" : "Not on sale"}
            </span>
          </div>

          {phase === "selection" && (
            <>
              <div className="flex flex-col">
                {event.ticket_types.map((tt, idx) => {
                  // Seats only become sellable once the event is published, and
                  // the counter is only meaningful then. Showing the raw 0 for a
                  // pending event read as "100% sold / sold out".
                  const onSale = event.event.status === "published";
                  const available = onSale ? tt.available : tt.quantity;
                  const soldCount = tt.quantity - available;
                  const soldPercent =
                    tt.quantity > 0
                      ? Math.round((soldCount / tt.quantity) * 100)
                      : 0;
                  const isLowStock = available > 0 && available < 5;
                  const isSoldOut = onSale && available === 0;
                  const max = Math.min(tt.max_per_order, available);

                  return (
                    <div
                      key={tt.id}
                      className={`py-3.5 space-y-2.5 ${idx > 0 ? "border-t border-[#E4EAF4]" : ""}`}
                    >
                      <div className="flex items-start justify-between gap-3">
                        <div>
                          <p className="text-sm font-bold text-[#16224A]">
                            {tt.name}
                          </p>
                          <p className="text-sm font-bold text-[#2450D9] mt-0.5">
                            {fmtIDR(tt.price_rupiah)}
                          </p>
                        </div>
                        {isSoldOut || !onSale ? (
                          <span className="badge badge-ink">
                            {isSoldOut ? "Sold out" : "Unavailable"}
                          </span>
                        ) : (
                          <Stepper
                            value={selectedTickets[tt.id] || 0}
                            max={max}
                            onChange={(v) =>
                              setSelectedTickets((p) => ({ ...p, [tt.id]: v }))
                            }
                          />
                        )}
                      </div>
                      <div className="space-y-1">
                        <div className="flex justify-between text-[11px] text-[#6E7B96]">
                          <span>
                            {isLowStock
                              ? `Only ${available} left!`
                              : `${soldPercent}% sold`}
                          </span>
                          <span>
                            {available} of {tt.quantity} available
                          </span>
                        </div>
                        <div className="w-full h-1.5 rounded-full bg-[#EDF1F7] overflow-hidden">
                          <div
                            className="h-full rounded-full bg-[#2450D9] transition-all"
                            style={{ width: `${soldPercent}%` }}
                          />
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>

              {orderItems.length > 0 && (
                <div className="border-t border-[#E4EAF4] pt-3 flex justify-between items-center">
                  <span className="text-sm font-semibold text-[#6E7B96]">
                    Subtotal
                  </span>
                  <span className="font-bold text-[#16224A]">
                    {fmtIDR(orderTotal)}
                  </span>
                </div>
              )}

              {!reservationsOpen && (
                <p className="text-center text-sm text-[#6E7B96]">
                  Reservations for this event are closed.
                </p>
              )}
              <button
                onClick={() => setPhase("summary")}
                disabled={!hasSelection || !hydrated || !user || !reservationsOpen}
                className="btn-accent w-full"
              >
                {!reservationsOpen
                  ? "Reservations Closed"
                  : hydrated && user
                    ? "Review Order"
                    : "Login to Reserve"}
              </button>
              <p className="flex items-center gap-1.5 text-xs font-bold text-[#2450D9]">
                <Timer className="w-3.5 h-3.5" />
                Kursi ditahan 30 menit setelah reserve
              </p>
              <p className="flex items-center gap-1.5 text-xs text-[#6E7B96] -mt-2">
                <ShieldCheck className="w-3.5 h-3.5" />
                Pembayaran aman via Xendit
              </p>
            </>
          )}

          {phase === "summary" && (
            <>
              <div className="flex flex-col gap-2.5">
                {orderItems.map((item) => (
                  <div
                    key={item.ticket_type_id}
                    className="flex justify-between text-sm"
                  >
                    <span className="text-[#3D4A66]">
                      {item.name}{" "}
                      <span className="text-[#9AA6BC]">
                        {item.quantity} &times; {fmtIDR(item.unit_price_rupiah)}
                      </span>
                    </span>
                    <span className="font-bold text-[#16224A]">
                      {fmtIDR(item.unit_price_rupiah * item.quantity)}
                    </span>
                  </div>
                ))}
              </div>
              <div className="border-t border-[#E4EAF4] pt-3 flex justify-between items-center">
                <span className="font-bold text-[#16224A]">Total</span>
                <span className="font-[family-name:var(--font-display)] text-2xl text-[#16224A]">
                  {fmtIDR(orderTotal)}
                </span>
              </div>
              <div className="flex gap-3">
                <button
                  onClick={() => setPhase("selection")}
                  className="btn-outline flex-1"
                >
                  <ChevronLeft className="w-4 h-4" /> Back
                </button>
                <button
                  onClick={() => setConfirming(true)}
                  disabled={reserveMutation.isPending || !reservationsOpen}
                  className="btn-accent flex-1"
                >
                  {reserveMutation.isPending ? (
                    <Loader2 className="w-4 h-4 animate-spin mx-auto" />
                  ) : (
                    <span className="flex items-center justify-center gap-2">
                      Checkout <ArrowRight className="w-4 h-4" />
                    </span>
                  )}
                </button>
              </div>
            </>
          )}
        </div>
      </div>

      <ConfirmDialog
        open={confirming}
        title="Reserve Tickets"
        message="This reservation is time-limited and cannot be cancelled. You'll continue to checkout to complete your payment."
        confirmLabel="Yes, Reserve"
        variant="warning"
        loading={reserveMutation.isPending}
        onConfirm={() => {
          setConfirming(false);
          handleCheckout();
        }}
        onClose={() => setConfirming(false)}
      />

      <ConfirmDialog
        open={approving}
        title="Approve Event"
        message={`Approve "${event?.event.title}" and make it public?`}
        confirmLabel="Yes, Approve"
        variant="success"
        onConfirm={() => {
          if (!event) return;
          adminApi
            .approveEvent(event.event.id)
            .then(() => {
              toast.success("Event approved");
              queryClient.invalidateQueries({ queryKey: ["event", id] });
              queryClient.invalidateQueries({ queryKey: ["adminEvents"] });
            })
            .catch((err: Error) => toast.error(err.message))
            .finally(() => setApproving(false));
        }}
        onClose={() => setApproving(false)}
      />

      <ConfirmDialog
        open={rejecting}
        title="Reject Event"
        message={`Reject "${event?.event.title}" and notify the organizer?`}
        confirmLabel="Reject"
        variant="danger"
        requireReason
        onConfirm={(reason) => {
          if (!event) return;
          adminApi
            .rejectEvent(event.event.id, reason || "")
            .then(() => {
              toast.success("Event rejected");
              queryClient.invalidateQueries({ queryKey: ["event", id] });
              queryClient.invalidateQueries({ queryKey: ["adminEvents"] });
            })
            .catch((err: Error) => toast.error(err.message))
            .finally(() => setRejecting(false));
        }}
        onClose={() => setRejecting(false)}
      />

      <ConfirmDialog
        open={cancelling}
        title="Cancel Event"
        message={`Are you sure you want to cancel "${event?.event.title}"? This action cannot be undone. All confirmed bookings will be cancelled and refunds issued.`}
        confirmLabel="Yes, Cancel"
        variant="danger"
        onConfirm={() => {
          if (!event) return;
          api
            .post<void>(`/api/events/${event.event.id}/cancel`)
            .then(() => {
              toast.success("Event cancelled");
              queryClient.invalidateQueries({ queryKey: ["event", id] });
              queryClient.invalidateQueries({ queryKey: ["myEvents"] });
            })
            .catch((err: Error) => toast.error(err.message))
            .finally(() => setCancelling(false));
        }}
        onClose={() => setCancelling(false)}
      />
    </div>
  );
}

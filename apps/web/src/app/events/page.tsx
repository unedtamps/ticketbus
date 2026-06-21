"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api } from "@/lib/api-client";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import { CalendarPlus, Plus, X } from "lucide-react";

interface TicketTypeInput {
  name: string;
  price_dollars: string;
  quantity: string;
  max_per_order: string;
}

function emptyTicketType(): TicketTypeInput {
  return { name: "", price_dollars: "", quantity: "", max_per_order: "5" };
}

export default function CreateEventPage() {
  const { user, loading: authLoading, hydrated, isEO } = useAuth();
  const router = useRouter();

  const [submitting, setSubmitting] = useState(false);
  const [success, setSuccess] = useState(false);

  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [venueName, setVenueName] = useState("");
  const [venueAddress, setVenueAddress] = useState("");
  const [venueCapacity, setVenueCapacity] = useState("");
  const [startAt, setStartAt] = useState("");
  const [endAt, setEndAt] = useState("");
  const [ticketTypes, setTicketTypes] = useState<TicketTypeInput[]>([emptyTicketType()]);

  useEffect(() => {
    if (!authLoading && !user) router.push("/login");
  }, [authLoading, user, router]);

  if (!hydrated) return null;
  if (!user) return null;
  if (!isEO) {
    return (
      <div className="card text-center py-16 max-w-md mx-auto">
        <CalendarPlus className="w-8 h-8 text-[#D4CEC4] mx-auto mb-3" />
        <p className="text-[#8B8580] text-sm">Only organizers can create events.</p>
      </div>
    );
  }

  function addTicketType() { setTicketTypes(p => [...p, emptyTicketType()]); }
  function removeTicketType(i: number) { setTicketTypes(p => p.filter((_, j) => j !== i)); }
  function updateTicketType(i: number, f: keyof TicketTypeInput, v: string) {
    setTicketTypes(p => p.map((tt, j) => j === i ? { ...tt, [f]: v } : tt));
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    const tt = ticketTypes.map(t => ({
      name: t.name,
      price_cents: Math.round(parseFloat(t.price_dollars || "0") * 100),
      quantity: parseInt(t.quantity, 10) || 0,
      max_per_order: parseInt(t.max_per_order, 10) || 5,
    }));
    try {
      await api.post("/api/events", {
        title, description,
        venue_name: venueName, venue_address: venueAddress,
        venue_capacity: parseInt(venueCapacity, 10) || 0,
        start_at: new Date(startAt).toISOString(),
        end_at: new Date(endAt).toISOString(),
        ticket_types: tt,
      });
      setSuccess(true);
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : "Failed to create event");
    } finally {
      setSubmitting(false);
    }
  }

  if (success) {
    return (
      <div className="max-w-md mx-auto text-center py-16">
        <div className="card-stub inline-block px-8 py-6 mb-6">
          <div className="stamp mx-auto">Submitted</div>
          <p className="font-[family-name:var(--font-display)] text-xl text-[#1A1817] mt-4 mb-2">Event created</p>
          <p className="text-[#8B8580] text-sm">Awaiting admin approval.</p>
        </div>
        <div className="flex gap-3 justify-center">
          <button onClick={() => router.push("/dashboard")} className="btn-outline">Dashboard</button>
          <button onClick={() => {
            setSuccess(false); setTitle(""); setDescription(""); setVenueName("");
            setVenueAddress(""); setVenueCapacity(""); setStartAt(""); setEndAt("");
            setTicketTypes([emptyTicketType()]);
          }} className="btn-accent">Create another</button>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-2xl mx-auto">
      <div className="mb-8">
        <p className="font-[family-name:var(--font-display)] text-3xl text-[#1A1817] mb-1">Create Event</p>
        <p className="text-[#8B8580] text-sm">Fill in the details for your new event</p>
      </div>

      <form onSubmit={handleSubmit} className="space-y-5">
        <div className="card space-y-4">
          <div>
            <label className="block text-sm font-medium text-[#4A4541] mb-1.5">Title</label>
            <input type="text" required value={title} onChange={e => setTitle(e.target.value)}
              className="input-field" placeholder="Summer Music Festival" />
          </div>
          <div>
            <label className="block text-sm font-medium text-[#4A4541] mb-1.5">Description</label>
            <textarea value={description} onChange={e => setDescription(e.target.value)}
              className="input-field" rows={3} placeholder="Optional description..." />
          </div>
        </div>

        <div className="card space-y-4">
          <p className="text-sm font-semibold text-[#4A4541] font-[family-name:var(--font-display)]">Venue</p>
          <div>
            <label className="block text-xs text-[#8B8580] mb-1">Name</label>
            <input type="text" required value={venueName} onChange={e => setVenueName(e.target.value)}
              className="input-field" placeholder="Madison Square Garden" />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs text-[#8B8580] mb-1">Address</label>
              <input type="text" required value={venueAddress} onChange={e => setVenueAddress(e.target.value)}
                className="input-field" placeholder="4 Pennsylvania Plaza, NY" />
            </div>
            <div>
              <label className="block text-xs text-[#8B8580] mb-1">Capacity</label>
              <input type="number" required min="1" value={venueCapacity} onChange={e => setVenueCapacity(e.target.value)}
                className="input-field" placeholder="20000" />
            </div>
          </div>
        </div>

        <div className="card space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs text-[#8B8580] mb-1">Start</label>
              <input type="datetime-local" required value={startAt} onChange={e => setStartAt(e.target.value)}
                className="input-field" />
            </div>
            <div>
              <label className="block text-xs text-[#8B8580] mb-1">End</label>
              <input type="datetime-local" required value={endAt} onChange={e => setEndAt(e.target.value)}
                className="input-field" />
            </div>
          </div>
        </div>

        <div className="card space-y-4">
          <div className="flex items-center justify-between">
            <p className="text-sm font-semibold text-[#4A4541] font-[family-name:var(--font-display)]">Ticket Types</p>
            <button type="button" onClick={addTicketType} className="flex items-center gap-1 text-xs font-medium text-[#D9381E] hover:text-[#B82E1A] transition-colors">
              <Plus className="w-3.5 h-3.5" /> Add
            </button>
          </div>
          <div className="space-y-3">
            {ticketTypes.map((tt, i) => (
              <div key={i} className="border border-dashed border-[#E8E3DC] rounded-md p-3 space-y-3">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-medium text-[#B0A89E] uppercase">Ticket {i + 1}</span>
                  {ticketTypes.length > 1 && (
                    <button type="button" onClick={() => removeTicketType(i)} className="text-[#D9381E]/60 hover:text-[#D9381E] transition-colors">
                      <X className="w-4 h-4" />
                    </button>
                  )}
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="block text-xs text-[#8B8580] mb-1">Name</label>
                    <input type="text" required value={tt.name} onChange={e => updateTicketType(i, "name", e.target.value)}
                      className="input-field text-sm" placeholder="General Admission" />
                  </div>
                  <div>
                    <label className="block text-xs text-[#8B8580] mb-1">Price ($)</label>
                    <input type="number" required min="0" step="0.01" value={tt.price_dollars}
                      onChange={e => updateTicketType(i, "price_dollars", e.target.value)}
                      className="input-field text-sm" placeholder="25.00" />
                  </div>
                  <div>
                    <label className="block text-xs text-[#8B8580] mb-1">Quantity</label>
                    <input type="number" required min="1" value={tt.quantity} onChange={e => updateTicketType(i, "quantity", e.target.value)}
                      className="input-field text-sm" placeholder="100" />
                  </div>
                  <div>
                    <label className="block text-xs text-[#8B8580] mb-1">Max per order</label>
                    <input type="number" min="1" value={tt.max_per_order} onChange={e => updateTicketType(i, "max_per_order", e.target.value)}
                      className="input-field text-sm" placeholder="5" />
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>

        <button type="submit" disabled={submitting} className="btn-accent w-full py-3 text-base">
          {submitting ? "Creating..." : "Create Event"}
        </button>
      </form>
    </div>
  );
}

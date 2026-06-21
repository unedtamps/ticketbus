"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import { adminApi, type PendingEvent } from "@/lib/admin-api";
import { Shield, Check, X, Calendar, MapPin, Users } from "lucide-react";
import { fmtShortDate } from "@/lib/format";

function RejectDialog({ event, onClose }: { event: PendingEvent; onClose: () => void }) {
  const [reason, setReason] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleReject() {
    if (!reason.trim()) return;
    setLoading(true);
    try { await adminApi.rejectEvent(event.id, reason); onClose(); }
    catch (err: unknown) { toast.error(err instanceof Error ? err.message : "Reject failed"); }
    finally { setLoading(false); }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-[#1A1817]/40 backdrop-blur-sm" onClick={onClose} />
      <div className="relative card w-full max-w-sm space-y-4 shadow-xl">
        <p className="font-[family-name:var(--font-display)] text-lg text-[#1A1817]">Reject Event</p>
        <p className="text-sm text-[#8B8580]">{event.title}</p>
        <div>
          <label className="block text-xs text-[#8B8580] mb-1">Reason</label>
          <textarea required value={reason} onChange={e => setReason(e.target.value)}
            className="input-field" rows={3} placeholder="e.g. Insufficient details..." />
        </div>
        <div className="flex gap-3 justify-end">
          <button onClick={onClose} className="btn-ghost text-sm">Cancel</button>
          <button onClick={handleReject} disabled={loading || !reason.trim()} className="btn-danger text-sm">
            {loading ? "Rejecting..." : "Reject"}
          </button>
        </div>
      </div>
    </div>
  );
}

export default function AdminPage() {
  const { isAdmin, user, hydrated } = useAuth();
  const router = useRouter();
  const [events, setEvents] = useState<PendingEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [rejecting, setRejecting] = useState<PendingEvent | null>(null);

  const fetchEvents = () => {
    adminApi.listPending()
      .then(d => setEvents(d.events || []))
      .catch(() => setEvents([]))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    if (!hydrated) return;
    if (!user) { router.push("/login"); return; }
    if (!isAdmin) { router.push("/dashboard"); return; }
    fetchEvents();
  }, [hydrated, user, isAdmin, router]);

  async function handleApprove(id: string) {
    try { await adminApi.approveEvent(id); setEvents(p => p.filter(e => e.id !== id)); }
    catch (err: unknown) { toast.error(err instanceof Error ? err.message : "Approve failed"); }
  }

  function handleRejectDone() { setRejecting(null); fetchEvents(); }

  if (!hydrated) {
    return (
      <div className="space-y-4">
        <div className="skeleton h-8 w-40" />
        <div className="skeleton h-4 w-56" />
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="card space-y-3">
            <div className="skeleton h-6 w-3/4" />
            <div className="skeleton h-4 w-1/2" />
            <div className="flex gap-2"><div className="skeleton h-8 w-24" /><div className="skeleton h-8 w-24" /></div>
          </div>
        ))}
      </div>
    );
  }

  if (loading) {
    return (
      <div className="space-y-4">
        <div className="skeleton h-8 w-40" />
        <div className="skeleton h-4 w-56" />
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="card space-y-3">
            <div className="skeleton h-6 w-3/4" />
            <div className="skeleton h-4 w-1/2" />
            <div className="flex gap-2"><div className="skeleton h-8 w-24" /><div className="skeleton h-8 w-24" /></div>
          </div>
        ))}
      </div>
    );
  }

  if (!isAdmin) return null;

  return (
    <div>
      <div className="mb-8">
        <div className="flex items-center gap-3 mb-2">
          <div className="w-10 h-10 rounded-lg bg-[#1A5DB8]/6 flex items-center justify-center">
            <Shield className="w-5 h-5 text-[#1A5DB8]" />
          </div>
          <div>
            <p className="font-[family-name:var(--font-display)] text-2xl text-[#1A1817]">Admin Panel</p>
            <p className="text-[#8B8580] text-sm">Review and approve pending events</p>
          </div>
        </div>
      </div>

      {events.length === 0 && (
        <div className="card text-center py-12">
          <Check className="w-8 h-8 text-[#2D7A46] mx-auto mb-3" />
          <p className="text-[#8B8580] text-sm mb-1">No pending events to review</p>
          <p className="text-[#B0A89E] text-xs">All caught up</p>
        </div>
      )}

      <div className="space-y-4">
        {events.map(event => (
          <div key={event.id} className="card-stub space-y-3">
            <div className="flex items-start justify-between">
              <div>
                <h3 className="font-[family-name:var(--font-display)] text-lg text-[#1A1817]">{event.title}</h3>
                <p className="text-sm text-[#8B8580] mt-1">{event.description || "No description"}</p>
              </div>
              <span className="badge badge-yellow">pending</span>
            </div>
            <div className="flex flex-wrap gap-4 text-xs text-[#8B8580]">
              <span className="flex items-center gap-1.5"><MapPin className="w-3.5 h-3.5 text-[#B0A89E]" />{event.venue_name}</span>
              <span className="flex items-center gap-1.5"><Users className="w-3.5 h-3.5 text-[#B0A89E]" />{event.venue_capacity}</span>
              <span className="flex items-center gap-1.5"><Calendar className="w-3.5 h-3.5 text-[#B0A89E]" />{fmtShortDate(event.start_at)}</span>
            </div>
            <div className="flex gap-2 pt-2 border-t border-dashed border-[#E8E3DC]">
              <button onClick={() => handleApprove(event.id)} className="btn-approve text-sm"><Check className="w-4 h-4" />Approve</button>
              <button onClick={() => setRejecting(event)} className="btn-danger text-sm"><X className="w-4 h-4" />Reject</button>
            </div>
          </div>
        ))}
      </div>

      {rejecting && <RejectDialog event={rejecting} onClose={handleRejectDone} />}
    </div>
  );
}

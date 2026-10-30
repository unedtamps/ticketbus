"use client";

import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { api } from "@/lib/api-client";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import { fmtIDR } from "@/lib/format";
import {
  CalendarPlus,
  Check,
  ChevronLeft,
  ChevronRight,
  ImagePlus,
  Plus,
  Shield,
  X,
} from "lucide-react";

interface TicketTypeInput {
  name: string;
  price_rupiah: string;
  quantity: string;
  max_per_order: string;
}

function emptyTicketType(): TicketTypeInput {
  return { name: "", price_rupiah: "", quantity: "", max_per_order: "5" };
}

const STEPS = ["Detail", "Tiket", "Review"] as const;
const MAX_POSTER_BYTES = 5 * 1024 * 1024;

export default function CreateEventPage() {
  const { user, hydrated, isEO } = useAuth();
  const router = useRouter();

  const [step, setStep] = useState(1);
  const [success, setSuccess] = useState(false);

  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [venueName, setVenueName] = useState("");
  const [venueAddress, setVenueAddress] = useState("");
  const [venueCapacity, setVenueCapacity] = useState("");
  const [startAt, setStartAt] = useState("");
  const [endAt, setEndAt] = useState("");
  const [ticketTypes, setTicketTypes] = useState<TicketTypeInput[]>(() => [emptyTicketType()]);
  const [posterFile, setPosterFile] = useState<File | null>(null);
  const [posterPreview, setPosterPreview] = useState<string | null>(null);

  // Keep the object-URL preview from leaking across replacements.
  useEffect(() => {
    if (!posterFile) {
      setPosterPreview(null);
      return;
    }
    const url = URL.createObjectURL(posterFile);
    setPosterPreview(url);
    return () => URL.revokeObjectURL(url);
  }, [posterFile]);

  const createMutation = useMutation({
    mutationFn: async (payload: Record<string, unknown>) => {
      const created = await api.post<{ id: string }>("/api/events", payload);
      // The s3 photo endpoint is per-event; upload after the event exists.
      // A failed poster upload must not fail the whole creation.
      if (posterFile) {
        const fd = new FormData();
        fd.append("photo", posterFile);
        try {
          await api.postForm(`/api/events/${created.id}/photo`, fd);
        } catch {
          toast.error("Event dibuat, tapi upload poster gagal — coba lagi dari dashboard.");
        }
      }
      return created;
    },
    onSuccess: () => setSuccess(true),
    onError: (err: Error) => toast.error(err.message),
  });

  if (!hydrated) return null;
  if (!isEO) {
    return (
      <div className="card text-center py-16 max-w-md mx-auto">
        <CalendarPlus className="w-8 h-8 text-[#C9D4E8] mx-auto mb-3" />
        <p className="text-[#6E7B96] text-sm">Only organizers can create events.</p>
      </div>
    );
  }

  function addTicketType() { setTicketTypes(p => [...p, emptyTicketType()]); }
  function removeTicketType(i: number) { setTicketTypes(p => p.filter((_, j) => j !== i)); }
  function updateTicketType(i: number, f: keyof TicketTypeInput, v: string) {
    setTicketTypes(p => p.map((tt, j) => j === i ? { ...tt, [f]: v } : tt));
  }

  function pickPoster(file: File | undefined) {
    if (!file) return;
    if (file.size > MAX_POSTER_BYTES) {
      toast.error("Poster maksimal 5 MB.");
      return;
    }
    setPosterFile(file);
  }

  // The end must be after the start: an event whose window is inverted can
  // never be booked (it is neither "not started yet" nor "not over").
  const windowInvalid =
    startAt !== "" && endAt !== "" && new Date(endAt) <= new Date(startAt);

  const detailValid =
    title.trim() !== "" &&
    venueName.trim() !== "" &&
    venueAddress.trim() !== "" &&
    venueCapacity !== "" &&
    startAt !== "" &&
    endAt !== "" &&
    !windowInvalid;

  const ticketsValid = ticketTypes.every(
    tt =>
      tt.name.trim() !== "" &&
      parseFloat(tt.price_rupiah || "0") >= 0 &&
      parseInt(tt.quantity, 10) > 0,
  );

  function goNext() {
    if (step === 1 && !detailValid) {
      toast.error("Lengkapi detail event dulu.");
      return;
    }
    if (step === 2 && !ticketsValid) {
      toast.error("Lengkapi nama, harga, dan jumlah tiap tipe tiket.");
      return;
    }
    setStep(s => Math.min(3, s + 1));
  }

  function buildPayload(): Record<string, unknown> {
    const tt = ticketTypes.map(t => ({
      name: t.name,
      // Input is in rupiah; the backend stores rupiah directly.
      price_rupiah: Math.round(parseFloat(t.price_rupiah || "0")),
      quantity: parseInt(t.quantity, 10) || 0,
      max_per_order: parseInt(t.max_per_order, 10) || 5,
    }));
    return {
      title, description,
      venue_name: venueName, venue_address: venueAddress,
      venue_capacity: parseInt(venueCapacity, 10) || 0,
      start_at: new Date(startAt).toISOString(),
      end_at: new Date(endAt).toISOString(),
      ticket_types: tt,
    };
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    createMutation.mutate(buildPayload());
  }

  if (success) {
    return (
      <div className="max-w-md mx-auto text-center py-16">
        <div className="card-stub inline-block px-8 py-6 mb-6">
          <div className="stamp mx-auto">Submitted</div>
          <p className="font-[family-name:var(--font-display)] text-xl text-[#16224A] mt-4 mb-2">Event created</p>
          <p className="text-[#6E7B96] text-sm">Awaiting admin approval.</p>
        </div>
        <div className="flex gap-3 justify-center">
          <button onClick={() => router.push("/dashboard")} className="btn-outline">Dashboard</button>
          <button onClick={() => {
            setSuccess(false); setStep(1); setTitle(""); setDescription(""); setVenueName("");
            setVenueAddress(""); setVenueCapacity(""); setStartAt(""); setEndAt("");
            setTicketTypes([emptyTicketType()]); setPosterFile(null);
          }} className="btn-accent">Create another</button>
        </div>
      </div>
    );
  }

  const totalQuota = ticketTypes.reduce((sum, t) => sum + (parseInt(t.quantity, 10) || 0), 0);

  const stepCircle = (n: number, label: string) => {
    const state = n < step ? "done" : n === step ? "active" : "todo";
    return (
      <button
        key={n}
        type="button"
        onClick={() => n < step && setStep(n)}
        disabled={n >= step}
        className={`flex items-center gap-2 text-sm ${n < step ? "cursor-pointer" : "cursor-default"}`}
      >
        <span
          className={`w-7 h-7 rounded-full flex items-center justify-center text-xs font-extrabold transition-colors ${
            state === "active"
              ? "bg-[#2450D9] text-white"
              : state === "done"
                ? "bg-[#E7EEFF] text-[#2450D9]"
                : "bg-white border border-[#E4EAF4] text-[#9AA6BC]"
          }`}
        >
          {state === "done" ? <Check className="w-3.5 h-3.5" /> : n}
        </span>
        <span
          className={`font-bold ${
            state === "todo" ? "text-[#9AA6BC]" : "text-[#16224A]"
          }`}
        >
          {n} &middot; {label}
        </span>
      </button>
    );
  };

  return (
    <div className="max-w-2xl mx-auto">
      <div className="mb-6">
        <p className="font-[family-name:var(--font-display)] text-3xl text-[#16224A] mb-1">Buat Event</p>
        <p className="text-[#6E7B96] text-sm">
          Tiga langkah sampai event masuk antrean approval admin
        </p>
      </div>

      <div className="flex items-center gap-6 mb-6">
        {STEPS.map((label, i) => stepCircle(i + 1, label))}
      </div>

      <form onSubmit={handleSubmit} className="space-y-5">
        {/* ── Step 1 · Detail ── */}
        {step === 1 && (
          <>
            <div className="card space-y-4">
              <div>
                <label className="block text-sm font-medium text-[#3D4A66] mb-1.5">Nama event</label>
                <input type="text" required value={title} onChange={e => setTitle(e.target.value)}
                  className="input-field" placeholder="Rooftop Jazz Malam Minggu" />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs text-[#6E7B96] mb-1">Venue</label>
                  <input type="text" required value={venueName} onChange={e => setVenueName(e.target.value)}
                    className="input-field" placeholder="Rooftop Alun-alun Bandung" />
                </div>
                <div>
                  <label className="block text-xs text-[#6E7B96] mb-1">Kota / Alamat</label>
                  <input type="text" required value={venueAddress} onChange={e => setVenueAddress(e.target.value)}
                    className="input-field" placeholder="Bandung" />
                </div>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs text-[#6E7B96] mb-1">Mulai</label>
                  <input type="datetime-local" required value={startAt} onChange={e => setStartAt(e.target.value)}
                    className="input-field" />
                </div>
                <div>
                  <label className="block text-xs text-[#6E7B96] mb-1">Selesai</label>
                  <input type="datetime-local" required value={endAt} onChange={e => setEndAt(e.target.value)}
                    className={`input-field ${windowInvalid ? "border-[#8A6400]" : ""}`} />
                </div>
              </div>
              {windowInvalid && (
                <p className="text-xs font-bold text-[#8A6400]">
                  Waktu selesai harus setelah waktu mulai.
                </p>
              )}
              <div>
                <label className="block text-xs text-[#6E7B96] mb-1">Kapasitas venue</label>
                <input type="number" required min="1" value={venueCapacity} onChange={e => setVenueCapacity(e.target.value)}
                  className="input-field" placeholder="600" />
              </div>
              <div>
                <label className="block text-sm font-medium text-[#3D4A66] mb-1.5">Deskripsi</label>
                <textarea value={description} onChange={e => setDescription(e.target.value)}
                  className="input-field" rows={3} placeholder="Malam jazz di atas rooftop — curated set dari musisi lokal…" />
              </div>
            </div>

            <div className="card space-y-3">
              <p className="text-sm font-semibold text-[#3D4A66] font-[family-name:var(--font-display)]">Poster event</p>
              <label className="block cursor-pointer">
                <input
                  type="file"
                  accept="image/jpeg,image/png,image/webp"
                  className="hidden"
                  onChange={e => {
                    pickPoster(e.target.files?.[0]);
                    e.target.value = "";
                  }}
                />
                {posterPreview ? (
                  <div className="flex items-center gap-4 border-2 border-dashed border-[#C9D4E8] rounded-xl p-3 hover:border-[#2450D9] transition-colors">
                    {/* eslint-disable-next-line @next/next/no-img-element */}
                    <img src={posterPreview} alt="" className="w-24 h-14 rounded-lg object-cover" />
                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-bold text-[#16224A] truncate">{posterFile?.name}</p>
                      <p className="text-xs text-[#6E7B96]">
                        {((posterFile?.size || 0) / (1024 * 1024)).toFixed(1)} MB — klik untuk ganti
                      </p>
                    </div>
                    <button
                      type="button"
                      onClick={(e) => { e.preventDefault(); setPosterFile(null); }}
                      className="btn-ghost"
                      aria-label="Hapus poster"
                    >
                      <X className="w-4 h-4" />
                    </button>
                  </div>
                ) : (
                  <div className="border-2 border-dashed border-[#C9D4E8] rounded-xl px-4 py-8 text-center hover:border-[#2450D9] hover:bg-[#F6F9FC] transition-colors">
                    <ImagePlus className="w-6 h-6 text-[#9AA6BC] mx-auto mb-2" />
                    <p className="text-sm font-semibold text-[#3D4A66]">
                      Letakkan poster di sini, atau klik untuk pilih file
                    </p>
                    <p className="text-xs text-[#6E7B96] mt-1">
                      PNG / JPG / WEBP &middot; maks 5 MB &middot; disarankan 1200 &times; 630
                    </p>
                  </div>
                )}
              </label>
              <p className="text-xs text-[#6E7B96]">Opsional — poster diupload setelah event dibuat.</p>
            </div>
          </>
        )}

        {/* ── Step 2 · Tiket ── */}
        {step === 2 && (
          <div className="card space-y-4">
            <div className="flex items-center justify-between">
              <p className="text-sm font-semibold text-[#3D4A66] font-[family-name:var(--font-display)]">Tipe tiket</p>
              <button type="button" onClick={addTicketType} className="flex items-center gap-1 text-xs font-bold text-[#2450D9] hover:text-[#1D3FAE] transition-colors">
                <Plus className="w-3.5 h-3.5" /> Tambah tipe tiket
              </button>
            </div>
            <div className="space-y-3">
              {ticketTypes.map((tt, i) => (
                <div key={i} className="border border-[#E4EAF4] rounded-xl p-3 space-y-3">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-bold text-[#9AA6BC] uppercase tracking-wide">Tiket {i + 1}</span>
                    {ticketTypes.length > 1 && (
                      <button type="button" onClick={() => removeTicketType(i)} className="text-[#9AA6BC] hover:text-[#16224A] transition-colors">
                        <X className="w-4 h-4" />
                      </button>
                    )}
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <label className="block text-xs text-[#6E7B96] mb-1">Nama</label>
                      <input type="text" required value={tt.name} onChange={e => updateTicketType(i, "name", e.target.value)}
                        className="input-field text-sm" placeholder="General Admission" />
                    </div>
                    <div>
                      <label className="block text-xs text-[#6E7B96] mb-1">Harga (Rp)</label>
                      <input type="number" required min="0" step="1" value={tt.price_rupiah}
                        onChange={e => updateTicketType(i, "price_rupiah", e.target.value)}
                        className="input-field text-sm" placeholder="150000" />
                    </div>
                    <div>
                      <label className="block text-xs text-[#6E7B96] mb-1">Jumlah</label>
                      <input type="number" required min="1" value={tt.quantity} onChange={e => updateTicketType(i, "quantity", e.target.value)}
                        className="input-field text-sm" placeholder="500" />
                    </div>
                    <div>
                      <label className="block text-xs text-[#6E7B96] mb-1">Maks per pesanan</label>
                      <input type="number" min="1" value={tt.max_per_order} onChange={e => updateTicketType(i, "max_per_order", e.target.value)}
                        className="input-field text-sm" placeholder="5" />
                    </div>
                  </div>
                </div>
              ))}
            </div>
            <p className="text-xs font-bold text-[#6E7B96]">Total kuota: {totalQuota} tiket</p>
          </div>
        )}

        {/* ── Step 3 · Review ── */}
        {step === 3 && (
          <>
            <div className="card space-y-3">
              <p className="text-sm font-semibold text-[#3D4A66] font-[family-name:var(--font-display)]">Detail</p>
              <div className="space-y-1.5 text-sm">
                <div className="flex justify-between gap-4"><span className="text-[#6E7B96]">Nama event</span><span className="font-bold text-[#16224A] text-right">{title}</span></div>
                <div className="flex justify-between gap-4"><span className="text-[#6E7B96]">Venue</span><span className="font-bold text-[#16224A] text-right">{venueName} · {venueAddress}</span></div>
                <div className="flex justify-between gap-4"><span className="text-[#6E7B96]">Kapasitas</span><span className="font-bold text-[#16224A]">{venueCapacity}</span></div>
                <div className="flex justify-between gap-4"><span className="text-[#6E7B96]">Waktu</span><span className="font-bold text-[#16224A] text-right">{startAt.replace("T", " ")} → {endAt.replace("T", " ")}</span></div>
                {posterFile && (
                  <div className="flex justify-between gap-4"><span className="text-[#6E7B96]">Poster</span><span className="font-bold text-[#16224A]">{posterFile.name}</span></div>
                )}
              </div>
            </div>

            <div className="card space-y-3">
              <p className="text-sm font-semibold text-[#3D4A66] font-[family-name:var(--font-display)]">Tiket</p>
              <div className="space-y-2">
                {ticketTypes.map((tt, i) => (
                  <div key={i} className="flex justify-between gap-4 text-sm">
                    <span className="text-[#6E7B96]">
                      {tt.name || `Tiket ${i + 1}`} &middot; {tt.quantity} tiket &middot; maks {tt.max_per_order}/pesanan
                    </span>
                    <span className="font-bold text-[#16224A] whitespace-nowrap">
                      {fmtIDR(Math.round(parseFloat(tt.price_rupiah || "0")))}
                    </span>
                  </div>
                ))}
              </div>
              <p className="text-xs font-bold text-[#6E7B96] pt-1 border-t border-[#E4EAF4]">
                Total kuota: {totalQuota} tiket
              </p>
            </div>

            <div className="flex items-start gap-2.5 rounded-xl bg-[#F0F5FF] px-4 py-3">
              <Shield className="w-4 h-4 text-[#2450D9] flex-shrink-0 mt-0.5" />
              <p className="text-[13px] font-semibold leading-relaxed text-[#16224A]">
                <span className="font-extrabold uppercase text-[11px] tracking-[1.4px] block mb-0.5 text-[#6E7B96]">Alur approval</span>
                Event yang diajukan masuk antrean admin. Setelah disetujui, event
                tayang dan tiket bisa langsung dijual.
              </p>
            </div>
          </>
        )}

        {/* ── Nav ── */}
        <div className="flex gap-3">
          {step > 1 && (
            <button type="button" onClick={() => setStep(s => s - 1)} className="btn-outline flex items-center gap-1.5">
              <ChevronLeft className="w-4 h-4" /> Kembali
            </button>
          )}
          {step < 3 ? (
            <button type="button" onClick={goNext} className="btn-accent flex-1 py-3 flex items-center justify-center gap-1.5">
              Lanjut <ChevronRight className="w-4 h-4" />
            </button>
          ) : (
            <button type="submit" disabled={createMutation.isPending} className="btn-accent flex-1 py-3 text-base">
              {createMutation.isPending ? "Mengajukan…" : "Ajukan untuk Approval"}
            </button>
          )}
        </div>
      </form>
    </div>
  );
}

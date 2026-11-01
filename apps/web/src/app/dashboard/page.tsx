"use client";

import { useQuery, useInfiniteQuery, useMutation, useQueryClient, useQueries } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { api } from "@/lib/api-client";
import { adminApi } from "@/lib/admin-api";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { PayoutAccountDialog } from "@/components/ui/payout-account-dialog";
import { Tooltip } from "@/components/ui/tooltip";
import { DashboardSidebar } from "@/components/layout/sidebar";
import {
  Building2,
  History,
  ImagePlus,
  Wallet,
  Shield,
  ShoppingBag,
  Ticket,
  CreditCard,
  ArrowRight,
  ChevronLeft,
  ChevronRight,
  Clock,
  Loader2,
  Check,
  Minus,
  Plus,
  MoreHorizontal,  X,
  RefreshCw,
  Calendar,
  MapPin,
  Users,
} from "lucide-react";
import { fmtIDR } from "@/lib/format";
import { LocalTime } from "@/components/ui/local-time";
import type { EventDetail, EventItem, EventPayout, OrganizerEventListResponse, OrganizerSummary, PayoutAccount, TransactionResponse } from "@/types";

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
  unit_price_rupiah: number;
  ticket_type_name?: string;
}

interface Booking {
  id: string;
  event_id: string;
  status: string;
  refund_status?: string;
  total_rupiah: number;
  created_at: string;
  items: BookingItem[];
  // Joined in by GET /api/bookings so the buyer can see what they bought.
  event_title?: string;
  event_start_at?: string;
  event_venue?: string;
}

// CollectAction renders the row's single payout affordance. Every state is
// derived from what is still owed (confirmed revenue minus paid out), so it
// reopens by itself when a late payment confirms after the event ended.
function CollectAction({
  event,
  ended,
  collectable,
  onCollect,
}: {
  event: EventItem;
  ended: boolean;
  collectable: number;
  onCollect: (e: EventItem) => void;
}) {
  const base =
    "flex items-center justify-center gap-1.5 w-full h-8 rounded-lg text-[11.5px] font-extrabold tabular-nums whitespace-nowrap";
  const inert = `${base} cursor-default select-none`;
  const hasSales = (event.confirmed_revenue_rupiah || 0) > 0;

  // In-flight payout blocks a second one; the backend would reject it.
  if (event.payout_status === "pending") {
    return (
      <Tooltip label="A payout request is being processed. Collect unlocks once the transfer settles.">
        <span className={`${inert} bg-[#FFF4CC] text-[#8A6400]`}>
          <Clock className="w-3.5 h-3.5" />
          Payout pending
        </span>
      </Tooltip>
    );
  }

  // Cancelled events are never collectable: RequestPayout requires published,
  // and the confirmed revenue still on the row is being refunded, not owed.
  if (event.status === "cancelled") {
    return (
      <Tooltip label="This event was cancelled. Sales are refunded in full, so there is nothing to collect.">
        <span className={`${inert} bg-[#EDF1F7] text-[#5F6B7E]`}>
          <Minus className="w-3.5 h-3.5" />
          Cancelled
        </span>
      </Tooltip>
    );
  }

  if (event.status === "published" && !ended) {
    return (
      <Tooltip label="Collect opens once the event ends.">
        <span className={`${inert} bg-[#F4F6FA] text-[#9AA6BC]`}>
          <Calendar className="w-3.5 h-3.5" />
          After event ends
        </span>
      </Tooltip>
    );
  }

  if (collectable <= 0) {
    return hasSales ? (
      <Tooltip label={`Fully paid. Reopens as "More" if a late payment confirms.`}>
        <span className={`${inert} bg-[#E4F5EA] text-[#1D7A45]`}>
          <Check className="w-3.5 h-3.5" />
          Fully paid
        </span>
      </Tooltip>
    ) : (
      <Tooltip label="This event had no confirmed bookings.">
        <span className={`${inert} bg-[#EDF1F7] text-[#5F6B7E]`}>
          <Minus className="w-3.5 h-3.5" />
          No sales
        </span>
      </Tooltip>
    );
  }

  // A prior payout means "More" (a top-up), otherwise this is the first collect.
  const isTopUp = (event.payout_amount_rupiah || 0) > 0;

  return (
    <Tooltip
      label={
        isTopUp
          ? "New confirmed earnings since the last payout. Collect the difference."
          : "Send this event's confirmed earnings to your payout account."
      }
    >
      <button
        onClick={() => onCollect(event)}
        className={`${base} transition-colors duration-150 ${
          isTopUp
            ? "bg-[#F0F5FF] text-[#2450D9] border border-[#C9D4E8] hover:bg-[#E7EEFF] hover:border-[#2450D9]"
            : "bg-[#2450D9] text-white hover:bg-[#1D3FAE]"
        }`}
      >
        {isTopUp ? <Plus className="w-3.5 h-3.5" /> : <Wallet className="w-3.5 h-3.5" />}
        {isTopUp ? "More" : "Collect"} {fmtIDR(collectable)}
      </button>
    </Tooltip>
  );
}

// EventRowMenu collects the row's secondary actions behind one overflow
// trigger. The row already carries a single primary action (Collect), so
// photo/history/cancel stay reachable without competing with it.
function EventRowMenu({
  event,
  uploading,
  hasHistory,
  historyOpen,
  onUpload,
  onToggleHistory,
  onCancel,
  onReprocess,
  ended,
}: {
  event: EventItem;
  uploading: boolean;
  hasHistory: boolean;
  historyOpen: boolean;
  onUpload: (eventID: string, file?: File) => void;
  onToggleHistory: (eventID: string) => void;
  onCancel: (e: EventItem) => void;
  onReprocess: (e: EventItem) => void;
  /** Cancelling is organizer-only before the event ends; afterwards an admin
   *  has to do it, so the option is not offered here. */
  ended: boolean;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  // Close on outside click / Escape so the menu behaves like a real popover.
  useEffect(() => {
    if (!open) return;
    function onDown(ev: MouseEvent) {
      if (ref.current && !ref.current.contains(ev.target as Node)) setOpen(false);
    }
    function onKey(ev: KeyboardEvent) {
      if (ev.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const canUpload = event.status !== "cancelled";
  // Past end_at, only an admin can cancel: an organizer who cancelled a
  // finished event would keep the payout and get every purchase refunded.
  const canCancel = event.status === "published" && !ended;
  const canReprocess = event.status === "cancelled";

  const itemCls =
    "flex w-full items-center gap-2 px-3 py-2 text-left text-xs font-bold text-[#3D4A66] hover:bg-[#F6F9FC] transition-colors first:rounded-t-lg last:rounded-b-lg";

  return (
    <div ref={ref} className="relative flex-shrink-0">
      <button
        onClick={() => setOpen(v => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="More actions"
        className={`flex items-center justify-center w-7 h-7 rounded-lg border transition-colors duration-150 ${
          open
            ? "border-[#C9D4E8] text-[#16224A] bg-[#F6F9FC]"
            : "border-[#E4EAF4] text-[#9AA6BC] hover:text-[#16224A] hover:border-[#C9D4E8]"
        }`}
      >
        <MoreHorizontal className="w-3.5 h-3.5" />
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 top-9 z-20 w-48 rounded-lg border border-[#E4EAF4] bg-white shadow-[0_8px_24px_rgba(22,34,74,0.10)] overflow-hidden"
        >
          {canUpload && (
            <button
              role="menuitem"
              onClick={() => fileRef.current?.click()}
              disabled={uploading}
              className={`${itemCls} disabled:opacity-50 disabled:cursor-not-allowed`}
            >
              <ImagePlus className="w-3.5 h-3.5 text-[#9AA6BC]" />
              {uploading ? "Uploading…" : event.image_url ? "Replace photo" : "Add photo"}
            </button>
          )}

          {hasHistory && (
            <button
              role="menuitem"
              onClick={() => {
                onToggleHistory(event.id);
                setOpen(false);
              }}
              className={itemCls}
            >
              <History className="w-3.5 h-3.5 text-[#9AA6BC]" />
              {historyOpen ? "Hide payout history" : "Payout history"}
            </button>
          )}

          {canReprocess && (
            <button
              role="menuitem"
              onClick={() => {
                onReprocess(event);
                setOpen(false);
              }}
              className={itemCls}
            >
              <RefreshCw className="w-3.5 h-3.5 text-[#2450D9]" />
              Reprocess refunds
            </button>
          )}

          {canCancel && (
            <button
              role="menuitem"
              onClick={() => {
                onCancel(event);
                setOpen(false);
              }}
              className={`${itemCls} text-[#16224A] hover:bg-[#EDF1F7]`}
            >
              <X className="w-3.5 h-3.5 text-[#9AA6BC]" />
              Cancel event
            </button>
          )}
        </div>
      )}

      <input
        ref={fileRef}
        type="file"
        accept="image/jpeg,image/png,image/webp"
        className="hidden"
        disabled={uploading}
        onChange={ev => {
          onUpload(event.id, ev.target.files?.[0]);
          ev.target.value = "";
          setOpen(false);
        }}
      />
    </div>
  );
}

// Page size for the organizer event table. Matches the backend default and
// stays well under its maxPageSize cap.
const MY_EVENTS_PAGE_SIZE = 10;

export default function DashboardPage() {
  const { user, hydrated, isAdmin, isEO, isCustomer } = useAuth();
  const queryClient = useQueryClient();

  const [adminFilter, setAdminFilter] = useState<AdminFilter>("pending");
  const [rejecting, setRejecting] = useState<EventItem | null>(null);
  const [cancelling, setCancelling] = useState<EventItem | null>(null);
  const [reprocessing, setReprocessing] = useState<EventItem | null>(null);
  const [approving, setApproving] = useState<EventItem | null>(null);
  const [accountDialogOpen, setAccountDialogOpen] = useState(false);
  const [payoutTarget, setPayoutTarget] = useState<EventItem | null>(null);
  const [historyFor, setHistoryFor] = useState<string | null>(null);
  // A stable render-time clock for the "event has ended" check.
  const [nowMs] = useState(() => Date.now());
  const [uploadingPhoto, setUploadingPhoto] = useState<string | null>(null);
  // Which customer booking row is expanded to show its ticket breakdown.
  const [expandedBooking, setExpandedBooking] = useState<string | null>(null);
  // Which loaded page of the organizer table is on screen (Prev/Next paging).
  const [myEventPageIndex, setMyEventPageIndex] = useState(0);

  const uploadPhoto = async (eventID: string, file?: File) => {
    if (!file) return;
    setUploadingPhoto(eventID);
    try {
      const fd = new FormData();
      fd.append("photo", file);
      await api.postForm(`/api/events/${eventID}/photo`, fd);
      toast.success("Event photo uploaded.");
      myEventsQuery.refetch();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Photo upload failed.");
    } finally {
      setUploadingPhoto(null);
    }
  };

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

  // EO query. Keyset-paginated: pageParam is the opaque next_cursor, empty on
  // the first page. Each page carries the whole-account summary, so the header
  // figures don't shrink as rows are appended.
  const myEventsQuery = useInfiniteQuery({
    queryKey: ["myEvents"],
    queryFn: ({ pageParam }) =>
      api.get<OrganizerEventListResponse>("/api/events/mine", {
        limit: MY_EVENTS_PAGE_SIZE,
        cursor: pageParam,
      }),
    initialPageParam: "",
    getNextPageParam: last => last.next_cursor || undefined,
    enabled: !!user && isEO,
  });

  // The event list carries the payout state; the attempt history is fetched
  // only when the organizer opens it.
  const payoutHistoryQuery = useQuery({
    queryKey: ["payoutHistory", historyFor],
    queryFn: () => api.get<EventPayout>(`/api/events/${historyFor}/payout`),
    enabled: !!historyFor,
  });

  const payoutAccountQuery = useQuery({
    queryKey: ["payoutAccount"],
    queryFn: () => api.get<PayoutAccount>("/api/payout-account"),
    enabled: !!user && isEO,
    retry: false,
  });
  const payoutAccount = payoutAccountQuery.data;

  const saveAccountMutation = useMutation({
    mutationFn: (values: {
      bank_code: string;
      bank_account_number: string;
      bank_account_holder_name: string;
    }) => api.put<PayoutAccount>("/api/payout-account", values),
    onSuccess: () => {
      toast.success("Payout account saved");
      queryClient.invalidateQueries({ queryKey: ["payoutAccount"] });
      setAccountDialogOpen(false);
    },
    onError: (err: Error) => toast.error(err.message),
  });

  const requestPayoutMutation = useMutation({
    mutationFn: (eventID: string) =>
      api.post<EventPayout>(`/api/events/${eventID}/payout`),
    onSuccess: () => {
      toast.success("Payout requested — we are preparing the transfer");
      queryClient.invalidateQueries({ queryKey: ["myEvents"] });
      setPayoutTarget(null);
    },
    onError: (err: Error) => toast.error(err.message),
  });

  // Admin query
  const adminEventsQuery = useQuery({
    queryKey: ["adminEvents", adminFilter],
    queryFn: () => adminApi.listAll(adminFilter, 50),
    enabled: !!user && isAdmin,
  });

  // Hooks must run unconditionally, so the pending-queue list is derived
  // before the early returns below.
  const adminEventsRaw = adminEventsQuery.data?.events || [];
  const moderationQueue = isAdmin && adminFilter === "pending";

  // The moderation queue shows per-event ticket totals and gross potential;
  // those live in the event detail, not the admin list response.
  const pendingDetailQueries = useQueries({
    queries: adminEventsRaw.map((e) => ({
      queryKey: ["event", e.id],
      queryFn: () => api.get<EventDetail>(`/api/events/${e.id}`),
      enabled: moderationQueue,
      staleTime: 60 * 1000,
    })),
  });
  const pendingDetails = new Map<string, EventDetail | undefined>();
  pendingDetailQueries.forEach((q, i) => {
    const e = adminEventsRaw[i];
    if (e) pendingDetails.set(e.id, q.data);
  });

  if (!hydrated) return null;
  if (!user) return null;

  const bookings = bookingsQuery.data || [];
  const transactions = transactionsQuery.data || [];
  // Prev/Next shows one page at a time. pages[] holds everything fetched so
  // far, so the visible one is tracked separately rather than assuming it is
  // the last (that breaks once you go backwards).
  const myEventPages = myEventsQuery.data?.pages || [];
  const currentPage = myEventPages[myEventPageIndex];
  const myEvents = currentPage?.events || [];
  // The summary is computed server-side over every event, so it stays correct
  // regardless of which page is showing.
  const myEventsSummary: OrganizerSummary | undefined = myEventPages[0]?.summary;
  const adminEvents = adminEventsRaw;
  const adminTotal = adminEventsQuery.data?.total || 0;

  // Paging state is derived from the page payloads, not from React Query's
  // hasNextPage/hasPreviousPage: those describe the position in the fetched
  // chain, not the page currently on screen.
  const canGoPrev = myEventPageIndex > 0;
  const canGoNext =
    myEventPageIndex < myEventPages.length - 1 || !!currentPage?.next_cursor;

  const goPrev = () => {
    if (myEventPageIndex > 0) setMyEventPageIndex(myEventPageIndex - 1);
  };

  const goNext = () => {
    const target = myEventPageIndex + 1;
    // Already fetched? Just show it. Otherwise pull it using this page's cursor.
    if (target < myEventPages.length) {
      setMyEventPageIndex(target);
      return;
    }
    if (!currentPage?.next_cursor || myEventsQuery.isFetchingNextPage) return;
    myEventsQuery.fetchNextPage().then(() => setMyEventPageIndex(target));
  };

  const isPaging =
    myEventsQuery.isFetchingNextPage || myEventsQuery.isFetchingPreviousPage;

  const eoLoading = myEventsQuery.isLoading;
  const adminLoading = adminEventsQuery.isLoading;

  // The base "badge" class carries the chip shape (uppercase, 6px radius,
  // 10px/800 type); the variant only supplies colours.
  const badgeClass = (s: string) =>
    "badge " + (
      s === "published" ? "badge-green" :
      s === "cancelled" || s === "rejected" ? "badge-red" :
      s === "pending" ? "badge-yellow" : "badge-ink"
    );

  // Still collectable = confirmed revenue minus what has already been paid out.
  // "More" means an earlier payout exists but new confirmed money has arrived
  // since (e.g. a late payment after the event ended).
  const collectableOf = (e: EventItem) =>
    Math.max(
      0,
      (e.confirmed_revenue_rupiah || 0) - (e.payout_amount_rupiah || 0),
    );

  // Header figures come from the server aggregate, not from the loaded rows —
  // otherwise they would only describe the pages fetched so far.
  const availableToCollect = myEventsSummary?.available_to_collect || 0;
  const collectableEvents = myEventsSummary?.collectable_events || 0;
  const totalConfirmed = myEventsSummary?.confirmed_total || 0;
  const totalPaidOut = myEventsSummary?.paid_out_total || 0;

  return (
    <div className="flex gap-8 items-start">
      <DashboardSidebar />

      <main className="flex-1 min-w-0">
        {/* Header */}
        <div id="dash-eo-top" className="mb-10 scroll-mt-24">
          <p className="font-[family-name:var(--font-display)] text-3xl text-[#16224A] mb-1">
            {isAdmin ? "Admin Dashboard" : "Dashboard"}
          </p>
          <p className="text-[#6E7B96] text-sm flex items-center gap-2">
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
                <span className="icon-tile"><Shield className="w-5 h-5" /></span>
                <div>
                  <p className="text-2xl font-extrabold text-[#16224A]">{adminTotal}</p>
                  <p className="text-xs font-semibold text-[#6E7B96]">Total Events</p>
                </div>
              </div>
              <Link href="/dashboard" onClick={() => setAdminFilter("pending")} className="card flex items-center gap-4 p-4 group cursor-pointer">
                <span className="icon-tile bg-[#FFF4CC] text-[#8A6400]"><Calendar className="w-5 h-5" /></span>
                <div>
                  <p className="text-2xl font-extrabold text-[#16224A]">
                    {adminFilter === "pending" ? adminTotal : "..."}
                  </p>
                  <p className="text-xs font-semibold text-[#6E7B96]">Pending Review</p>
                </div>
              </Link>
              <div className="card flex items-center gap-4 p-4">
                <span className="icon-tile"><Check className="w-5 h-5" /></span>
                <div>
                  <p className="text-lg font-extrabold text-[#16224A] capitalize">
                    {adminFilter ? adminFilter : "all"}
                  </p>
                  <p className="text-xs font-semibold text-[#6E7B96]">Current Filter</p>
                </div>
              </div>
            </div>

            {/* Filter chips */}
            <div className="flex gap-2 mb-6 overflow-x-auto pb-2">
              {FILTER_LABELS.map(f => (
                <button
                  key={f.key}
                  onClick={() => setAdminFilter(f.key)}
                  className={`text-sm font-bold px-4 py-1.5 rounded-full transition-colors duration-150 whitespace-nowrap ${
                    adminFilter === f.key
                      ? "bg-[#2450D9] text-white"
                      : "text-[#3D4A66] hover:bg-[#F6F9FC]"
                  }`}
                >
                  {f.label}
                </button>
              ))}
            </div>

            {/* Loading */}
            {adminLoading && (
              <div className="card text-center py-10">
                <Loader2 className="w-5 h-5 text-[#C9D4E8] animate-spin mx-auto mb-2" />
                <p className="text-[#6E7B96] text-sm">Loading events...</p>
              </div>
            )}

            {/* Empty */}
            {!adminLoading && adminEvents.length === 0 && (
              <div className="card text-center py-12">
                <Ticket className="w-8 h-8 text-[#C9D4E8] mx-auto mb-3" />
                <p className="text-[#6E7B96] text-sm">No events found for this filter</p>
              </div>
            )}

            {/* Moderation queue (pending filter) — per design screen 09 */}
            {!adminLoading && moderationQueue && adminEvents.length > 0 && (
              <div id="dash-admin-events" className="card p-0 overflow-hidden scroll-mt-24">
                <div className="px-5 pt-5 pb-4">
                  <div className="flex items-center justify-between gap-3">
                    <p className="font-[family-name:var(--font-display)] text-xl text-[#16224A]">
                      Menunggu persetujuan
                    </p>
                    <span className="badge chip-citrine">{adminTotal} EVENT</span>
                  </div>
                  <p className="text-[13px] text-[#6E7B96] mt-1">
                    Event baru tayang setelah kamu setujui. Cek poster &amp; data sebelum memutuskan.
                  </p>
                </div>
                <div className="divide-y divide-[#E4EAF4] border-t border-[#E4EAF4]">
                  {adminEvents.map(event => {
                    const detail = pendingDetails.get(event.id);
                    const types = detail?.ticket_types || [];
                    const quota = types.reduce((sum, tt) => sum + tt.quantity, 0);
                    const gross = types.reduce((sum, tt) => sum + tt.price_rupiah * tt.quantity, 0);
                    const initials = event.title
                      .split(/\s+/)
                      .slice(0, 2)
                      .map(w => w[0]!.toUpperCase())
                      .join("");
                    return (
                      <div key={event.id} className="flex items-center gap-3.5 px-5 py-3.5">
                        {event.image_url ? (
                          // eslint-disable-next-line @next/next/no-img-element
                          <img src={event.image_url} alt="" className="w-[52px] h-[52px] rounded-xl object-cover flex-shrink-0" />
                        ) : (
                          <div className="w-[52px] h-[52px] rounded-xl bg-[#F0F5FF] text-[#2450D9] flex items-center justify-center font-[family-name:var(--font-display)] text-lg flex-shrink-0">
                            {initials}
                          </div>
                        )}
                        <div className="flex-1 min-w-0">
                          <Link
                            href={`/events/${event.id}`}
                            className="block text-[13.5px] font-bold text-[#16224A] hover:text-[#2450D9] transition-colors truncate"
                          >
                            {event.title}
                          </Link>
                          <p className="text-[11.5px] font-semibold text-[#6E7B96] mt-0.5 truncate">
                            <LocalTime iso={event.start_at} format="short" /> &middot; {event.venue_name || event.venue_address}
                          </p>
                        </div>
                        <div className="hidden md:block text-right flex-shrink-0">
                          <p className="text-[10px] font-extrabold uppercase tracking-[1.2px] text-[#6E7B96]">
                            {detail ? `${quota} tiket · ${types.length} tipe` : "…"}
                          </p>
                          <p className="text-[10px] font-extrabold uppercase tracking-[1.2px] text-[#6E7B96] mt-0.5">
                            Potensi gross
                          </p>
                          <p className="text-[13px] font-extrabold text-[#16224A]">
                            {detail ? fmtIDR(gross) : "…"}
                          </p>
                        </div>
                        <div className="flex gap-2 flex-shrink-0">
                          <button onClick={() => setRejecting(event)} className="btn-outline text-xs font-bold px-3.5! py-2!">
                            <X className="w-3.5 h-3.5" /> Tolak
                          </button>
                          <button onClick={() => setApproving(event)} className="btn-approve text-xs px-3.5! py-2!">
                            <Check className="w-3.5 h-3.5" /> Setujui
                          </button>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            )}

            {/* Event cards (other filters) */}
            {!adminLoading && !moderationQueue && adminEvents.length > 0 && (
              <div id="dash-admin-events" className="space-y-4 scroll-mt-24">
                {adminEvents.map(event => (
                  <div key={event.id} className="card space-y-3">
                    <div className="flex items-start justify-between">
                      <div>
                        <h3 className="font-[family-name:var(--font-display)] text-lg text-[#16224A]">{event.title}</h3>
                        <p className="text-sm text-[#6E7B96] mt-1 line-clamp-1">{event.description || "No description"}</p>
                      </div>
                      <span className={badgeClass(event.status)}>{event.status}</span>
                    </div>
                    <div className="flex flex-wrap gap-4 text-xs font-semibold text-[#6E7B96]">
                      {event.venue_name ? (
                        <span className="flex items-center gap-1.5"><MapPin className="w-3.5 h-3.5 text-[#9AA6BC]" />{event.venue_name}</span>
                      ) : null}
                      {event.venue_capacity > 0 && (
                        <span className="flex items-center gap-1.5"><Users className="w-3.5 h-3.5 text-[#9AA6BC]" />{event.venue_capacity}</span>
                      )}
                      <span className="flex items-center gap-1.5"><Calendar className="w-3.5 h-3.5 text-[#9AA6BC]" /><LocalTime iso={event.start_at} format="short" /></span>
                    </div>
                    <div className="flex gap-2 pt-3 border-t border-[#E4EAF4]">
                      <Link href={`/events/${event.id}`} className="btn-outline text-sm">View</Link>
                      {event.status === "pending" && (
                        <>
                          <button onClick={() => setApproving(event)} className="btn-approve text-sm">
                            <Check className="w-4 h-4" />Approve
                          </button>
                          <button onClick={() => setRejecting(event)} className="btn-outline text-sm font-bold text-[#16224A]"><X className="w-4 h-4" />Reject</button>
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
            {/* Primary action + payout summary. The old three-tile grid gave a
                stat, a link and a settings shortcut identical weight; this
                promotes the one action and keeps the two figures quiet. */}
            <div id="dash-payouts" className="grid gap-4 lg:grid-cols-[1.35fr_1fr] mb-10 fade-up scroll-mt-24 items-stretch">
              {/* Create Event — the page's primary action, so it gets the solid
                  royal panel rather than the same white card as the metrics. */}
              <Link
                href="/events"
                className="relative flex flex-col justify-between overflow-hidden rounded-2xl bg-[#2450D9] p-6 text-white transition-colors duration-200 hover:bg-[#1D3FAE] group"
              >
                {/* Dashed citrine target ring, decorative only. */}
                <span
                  aria-hidden
                  className="pointer-events-none absolute -right-[26px] -top-[26px] w-[104px] h-[104px] rounded-full border-[1.5px] border-dashed border-[#FFD43B]/55"
                >
                  <span className="absolute inset-[22px] rounded-full bg-[#FFD43B]/15" />
                </span>

                <div className="relative">
                  <p className="text-[10.5px] font-extrabold uppercase tracking-[0.14em] text-[#FFD43B] mb-2.5">
                    Next step
                  </p>
                  <p className="font-[family-name:var(--font-display)] text-[27px] leading-[1.15] tracking-[-0.015em]">
                    Create Event
                  </p>
                  <p className="text-[13px] font-semibold text-white/78 mt-1.5">
                    Draft, price, and submit for approval.
                  </p>
                </div>

                <span className="relative mt-6 inline-flex items-center gap-2 self-start rounded-lg bg-white px-3.5 py-2 text-[12.5px] font-extrabold text-[#2450D9]">
                  Set up a new event
                  <ArrowRight className="w-3.5 h-3.5 transition-transform duration-200 group-hover:translate-x-0.5" />
                </span>
              </Link>

              <div className="card p-0 overflow-hidden">
                <div className="flex items-start justify-between gap-4 px-5 py-4 border-b border-[#E4EAF4]">
                  <div className="min-w-0">
                    <p className="text-[10.5px] font-extrabold uppercase tracking-[0.1em] text-[#9AA6BC] mb-1">
                      Available to collect
                    </p>
                    <p className="font-[family-name:var(--font-display)] text-2xl text-[#2450D9] leading-none">
                      {fmtIDR(availableToCollect)}
                    </p>
                    <p className="text-[11.5px] font-semibold text-[#6E7B96] mt-1.5">
                      {collectableEvents > 0
                        ? `From ${collectableEvents} ended event${collectableEvents === 1 ? "" : "s"}`
                        : "Nothing waiting right now"}
                    </p>
                  </div>
                  <button
                    onClick={() => setAccountDialogOpen(true)}
                    className="flex items-center gap-1.5 text-[11.5px] font-bold px-2.5 py-1.5 rounded-lg border border-[#E4EAF4] bg-white text-[#3D4A66] hover:bg-[#F6F9FC] hover:border-[#C9D4E8] transition-colors duration-150 flex-shrink-0"
                  >
                    <Building2 className="w-3.5 h-3.5" />
                    {payoutAccount
                      ? `${payoutAccount.bank_code} ····${payoutAccount.bank_account_number.slice(-4)}`
                      : "Set up bank"}
                  </button>
                </div>
                <div className="px-5 py-3">
                  <div className="flex items-baseline justify-between gap-3">
                    <p className="text-[10.5px] font-extrabold uppercase tracking-[0.1em] text-[#9AA6BC]">
                      Total earned
                    </p>
                    <p className="text-[13px] font-extrabold text-[#16224A] tabular-nums">
                      {fmtIDR(totalConfirmed)}
                    </p>
                  </div>
                  <div className="flex items-baseline justify-between gap-3 mt-1">
                    <p className="text-[10.5px] font-extrabold uppercase tracking-[0.1em] text-[#9AA6BC]">
                      Paid out
                    </p>
                    <p className="text-xs font-bold text-[#6E7B96] tabular-nums">
                      {fmtIDR(totalPaidOut)}
                    </p>
                  </div>
                </div>
              </div>
            </div>

            <p id="dash-my-events" className="font-[family-name:var(--font-display)] text-xl text-[#16224A] mb-5 scroll-mt-24">My Events</p>

            {eoLoading && (
              <div className="card text-center py-10">
                <Loader2 className="w-5 h-5 text-[#C9D4E8] animate-spin mx-auto mb-2" />
                <p className="text-[#6E7B96] text-sm">Loading events...</p>
              </div>
            )}

            {!eoLoading && myEvents.length === 0 && (
              <div className="card text-center py-12">
                <Ticket className="w-8 h-8 text-[#C9D4E8] mx-auto mb-3" />
                <p className="text-[#6E7B96] text-sm mb-2">No events yet</p>
                <Link href="/events" className="text-[#2450D9] text-sm font-bold hover:underline">
                  Create your first event &rarr;
                </Link>
              </div>
            )}

            {historyFor && (
              <div className="card p-4 space-y-3 mb-3">
                <div className="flex items-center justify-between">
                  <p className="text-sm font-bold text-[#16224A]">
                    Payout attempts
                  </p>
                  <button
                    onClick={() => setHistoryFor(null)}
                    className="text-xs text-[#6E7B96] hover:text-[#3D4A66]"
                  >
                    Close
                  </button>
                </div>
                {payoutHistoryQuery.isLoading && (
                  <p className="text-xs text-[#6E7B96]">Loading attempts...</p>
                )}
                {payoutHistoryQuery.data && (
                  <>
                    <p className="text-xs font-semibold text-[#6E7B96]">
                      Total paid out: {fmtIDR(payoutHistoryQuery.data.amount_rupiah)} over{" "}
                      {payoutHistoryQuery.data.attempts} attempt
                      {payoutHistoryQuery.data.attempts === 1 ? "" : "s"}
                    </p>
                    <div className="space-y-2">
                      {(payoutHistoryQuery.data.attempts_list || []).map((a) => (
                        <div
                          key={a.attempt}
                          className="flex items-center justify-between text-xs"
                        >
                          <span className="text-[#3D4A66] font-semibold">
                            Attempt {a.attempt} · <LocalTime iso={a.requested_at} format="short" />
                          </span>
                          <span className="flex items-center gap-2">
                            <span className="text-[#16224A] font-bold">
                              {fmtIDR(a.amount_rupiah)}
                            </span>
                            <span
                              className={`badge ${
                                a.status === "paid_out"
                                  ? "badge-green"
                                  : a.status === "failed"
                                    ? "badge-red"
                                    : "badge-yellow"
                              }`}
                            >
                              {a.status}
                            </span>
                          </span>
                        </div>
                      ))}
                    </div>
                  </>
                )}
              </div>
            )}

            {!eoLoading && myEvents.length > 0 && (
              <div className="rounded-xl border border-[#E4EAF4] overflow-hidden bg-white">
                {/* Header labels the columns once, so rows carry no mini-labels. */}
                <div className="grid grid-cols-[1fr_96px_132px_178px_28px] gap-3 items-center px-4 py-2.5 bg-[#F6F9FC] border-b border-[#E4EAF4]">
                  <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC]">Event</span>
                  <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC]">Status</span>
                  <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC] text-right">Total earning</span>
                  <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC]">Collect</span>
                  <span />
                </div>

                {myEvents.map(e => {
                  const payoutStatus = e.payout_status || "none";
                  const ended = new Date(e.end_at).getTime() < nowMs;
                  return (
                    <div
                      key={e.id}
                      className={`grid grid-cols-[1fr_96px_132px_178px_28px] gap-3 items-center px-4 py-3.5 border-l-[3px] transition-colors hover:bg-[#FCFDFF] ${
                        e.status === "published" && !ended ? "border-l-[#2E9E5B]" :
                        e.status === "published" ? "border-l-[#2450D9]" :
                        e.status === "pending" ? "border-l-[#FFD43B]" : "border-l-[#EDF1F7]"
                      }`}
                    >
                      <Link href={`/events/${e.id}`} className="group min-w-0">
                        <p className="text-sm font-extrabold text-[#16224A] group-hover:text-[#2450D9] transition-colors truncate">{e.title}</p>
                        <p className="text-[11.5px] font-semibold text-[#6E7B96] mt-0.5 truncate">
                          <LocalTime iso={e.start_at} format="short" /> &middot; {e.venue_name}
                        </p>
                      </Link>

                      <span className={badgeClass(e.status)}>{e.status}</span>

                      {/* Total earning — the event's confirmed revenue. */}
                      <div className="text-right">
                        <p className={`text-[13px] font-extrabold tabular-nums ${
                          (e.confirmed_revenue_rupiah || 0) > 0 ? "text-[#16224A]" : "text-[#9AA6BC]"
                        }`}>
                          {fmtIDR(e.confirmed_revenue_rupiah || 0)}
                        </p>
                        <p className={`text-[9.5px] font-bold uppercase tracking-[0.06em] mt-0.5 ${
                          e.status === "published" && !ended ? "text-[#1D7A45]" : "text-[#9AA6BC]"
                        }`}>
                          {e.status === "published" && !ended ? "so far" : "confirmed"}
                        </p>
                      </div>

                      {/* Collect — one action, driven purely by what is still owed. */}
                      <CollectAction
                        event={e}
                        ended={ended}
                        collectable={collectableOf(e)}
                        onCollect={setPayoutTarget}
                      />

                      {/* Secondary actions collapse into an overflow menu so the
                          row keeps the single primary action from the design. */}
                      <EventRowMenu
                        event={e}
                        uploading={uploadingPhoto === e.id}
                        hasHistory={payoutStatus !== "none"}
                        historyOpen={historyFor === e.id}
                        onUpload={uploadPhoto}
                        onToggleHistory={id => setHistoryFor(historyFor === id ? null : id)}
                        onCancel={setCancelling}
                        onReprocess={setReprocessing}
                        ended={ended}
                      />
                    </div>
                  );
                })}

                {/* Paging footer. Cursor pagination cannot say "page 2 of 7",
                    so the controls sit at the right and the count is gone. */}
                {(canGoPrev || canGoNext) && (
                  <div className="flex items-center justify-end gap-2 px-4 py-3 border-t border-[#E4EAF4] bg-[#FCFDFF]">
                    <button
                      onClick={goPrev}
                      disabled={!canGoPrev || isPaging}
                      aria-label="Previous page"
                      className="flex items-center gap-1 rounded-lg border border-[#E4EAF4] bg-white px-3 py-1.5 text-[11.5px] font-extrabold text-[#3D4A66] transition-colors duration-150 hover:bg-[#F6F9FC] hover:border-[#C9D4E8] disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-white"
                    >
                      <ChevronLeft className="w-3.5 h-3.5" />
                      Prev
                    </button>
                    <button
                      onClick={goNext}
                      disabled={!canGoNext || isPaging}
                      aria-label="Next page"
                      className="flex items-center gap-1 rounded-lg border border-[#C9D4E8] bg-white px-3 py-1.5 text-[11.5px] font-extrabold text-[#2450D9] transition-colors duration-150 hover:bg-[#F0F5FF] disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-white"
                    >
                      {isPaging ? (
                        <>
                          Loading
                          <Loader2 className="w-3.5 h-3.5 animate-spin" />
                        </>
                      ) : (
                        <>
                          Next
                          <ChevronRight className="w-3.5 h-3.5" />
                        </>
                      )}
                    </button>
                  </div>
                )}
              </div>
            )}

            {/* Payouts — its own destination. The sidebar "Payouts" entry used to
                scroll back to the summary strip at the top of the page, so every
                EO nav item pointed at the same block. */}
            <section id="dash-payout-summary" className="mt-10 rounded-xl border border-[#E4EAF4] overflow-hidden scroll-mt-24">
              <div className="flex items-center justify-between gap-4 px-5 py-4 bg-[#F6F9FC] border-b border-[#E4EAF4]">
                <p className="font-[family-name:var(--font-display)] text-lg text-[#16224A]">Payouts</p>
                <div className="flex items-center gap-2 text-xs font-bold text-[#3D4A66]">
                  <span className="flex items-center justify-center w-6.5 h-6.5 px-1.5 py-1 rounded-md bg-white border border-[#E4EAF4] text-[8.5px] font-extrabold text-[#2450D9]">
                    {payoutAccount?.bank_code || "—"}
                  </span>
                  {payoutAccount
                    ? `${payoutAccount.bank_account_holder_name} · ····${payoutAccount.bank_account_number.slice(-4)}`
                    : "No payout account yet"}
                </div>
              </div>

              <div className="grid sm:grid-cols-2">
                <div className="px-5 py-4 sm:border-r border-[#E4EAF4]">
                  <p className="text-[10.5px] font-extrabold uppercase tracking-[0.1em] text-[#9AA6BC] mb-1.5">
                    Available to collect
                  </p>
                  <p className="font-[family-name:var(--font-display)] text-2xl text-[#2450D9] leading-none">
                    {fmtIDR(availableToCollect)}
                  </p>
                  <p className="text-[11.5px] font-semibold text-[#6E7B96] mt-1.5">
                    {collectableEvents > 0
                      ? `${collectableEvents} event${collectableEvents === 1 ? "" : "s"} ready · collect per event above`
                      : "Nothing waiting right now"}
                  </p>
                </div>
                <div className="px-5 py-4">
                  <p className="text-[10.5px] font-extrabold uppercase tracking-[0.1em] text-[#9AA6BC] mb-1.5">
                    Paid out lifetime
                  </p>
                  <p className="font-[family-name:var(--font-display)] text-2xl text-[#16224A] leading-none">
                    {fmtIDR(totalPaidOut)}
                  </p>
                  <p className="text-[11.5px] font-semibold text-[#6E7B96] mt-1.5">
                    From {fmtIDR(totalConfirmed)} confirmed
                  </p>
                </div>
              </div>

              <div className="flex items-center justify-between gap-4 px-5 py-3 border-t border-[#E4EAF4]">
                <p className="text-[11.5px] font-semibold text-[#6E7B96]">
                  Collect is per event. &ldquo;Fully paid&rdquo; reopens as &ldquo;More&rdquo; if a late payment confirms.
                </p>
                <button
                  onClick={() => setAccountDialogOpen(true)}
                  className="flex items-center gap-1.5 text-xs font-extrabold px-3 py-2 rounded-lg border border-[#C9D4E8] text-[#2450D9] bg-white hover:bg-[#F0F5FF] transition-colors duration-150 flex-shrink-0"
                >
                  <Building2 className="w-3.5 h-3.5" />
                  Edit bank account
                </button>
              </div>
            </section>
          </>
        )}

        {/* ── CUSTOMER ── */}
        {isCustomer && (
          <>
            <div className="grid gap-4 sm:grid-cols-2 mb-10 fade-up">
              <div className="card flex items-center gap-4 p-4">
                <span className="icon-tile"><ShoppingBag className="w-5 h-5" /></span>
                <div>
                  <p className="text-sm font-bold text-[#16224A]">My Tickets</p>
                  <p className="text-xs font-semibold text-[#6E7B96]">
                    {bookings.length} booking{bookings.length !== 1 ? "s" : ""}
                  </p>
                </div>
              </div>
              <div className="card flex items-center gap-4 p-4">
                <span className="icon-tile"><CreditCard className="w-5 h-5" /></span>
                <div>
                  <p className="text-sm font-bold text-[#16224A]">Payments</p>
                  <p className="text-xs font-semibold text-[#6E7B96]">
                    {transactions.length} transaction{transactions.length !== 1 ? "s" : ""}
                  </p>
                </div>
              </div>
            </div>

            {/* ── Tickets table ── */}
            <div className="flex items-center justify-between gap-4 mb-4">
              <p id="dash-tickets" className="font-[family-name:var(--font-display)] text-xl text-[#16224A] scroll-mt-24">
                My Tickets
              </p>
              <p className="text-[11.5px] font-semibold text-[#9AA6BC]">
                Tap a row to see the tickets
              </p>
            </div>

            {bookingsQuery.isLoading && (
              <div className="card text-center py-10">
                <Loader2 className="w-5 h-5 text-[#C9D4E8] animate-spin mx-auto mb-2" />
                <p className="text-[#6E7B96] text-sm">Loading tickets...</p>
              </div>
            )}

            {!bookingsQuery.isLoading && bookings.length === 0 && (
              <div className="card text-center py-12">
                <ShoppingBag className="w-8 h-8 text-[#C9D4E8] mx-auto mb-3" />
                <p className="text-[#6E7B96] text-sm mb-2">No bookings yet</p>
                <Link href="/" className="text-[#2450D9] text-sm font-bold hover:underline">
                  Browse events &rarr;
                </Link>
              </div>
            )}

            {!bookingsQuery.isLoading && bookings.length > 0 && (
              <div className="rounded-xl border border-[#E4EAF4] overflow-hidden bg-white">
                <div className="grid grid-cols-[1fr_120px_140px_44px] gap-3 items-center px-4 py-2.5 bg-[#F6F9FC] border-b border-[#E4EAF4]">
                  <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC]">Event</span>
                  <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC]">Date</span>
                  <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC] text-right">Total</span>
                  <span />
                </div>

                {bookings.map(b => {
                  const isOpen = expandedBooking === b.id;
                  const ticketCount = b.items.reduce((n, i) => n + i.quantity, 0);
                  return (
                    <div key={b.id} className="border-b border-[#E4EAF4] last:border-b-0">
                      <button
                        onClick={() => setExpandedBooking(isOpen ? null : b.id)}
                        aria-expanded={isOpen}
                        className="grid w-full grid-cols-[1fr_120px_140px_44px] gap-3 items-center px-4 py-3.5 text-left transition-colors hover:bg-[#FCFDFF]"
                      >
                        <div className="min-w-0">
                          <p className="truncate text-sm font-extrabold text-[#16224A]">
                            {b.event_title || `Booking ${b.id.slice(0, 8)}`}
                          </p>
                          <p className="mt-0.5 truncate text-[11.5px] font-semibold text-[#6E7B96]">
                            {b.event_venue || "—"} &middot; {ticketCount} ticket{ticketCount === 1 ? "" : "s"}
                          </p>
                        </div>
                        <div className="text-[11.5px] font-semibold text-[#6E7B96]">
                          {b.event_start_at
                            ? <LocalTime iso={b.event_start_at} format="short" />
                            : <LocalTime iso={b.created_at} format="short" />}
                        </div>
                        <div className="text-right">
                          <p className="text-[13px] font-extrabold tabular-nums text-[#16224A]">{fmtIDR(b.total_rupiah)}</p>
                          <span className={`badge mt-1 ${
                            b.status === "confirmed" ? "badge-green" :
                            b.status === "pending" ? "badge-yellow" : "badge-red"
                          }`}>{b.status}</span>
                        </div>
                        <ChevronRight className={`w-4 h-4 justify-self-center text-[#9AA6BC] transition-transform duration-200 ${isOpen ? "rotate-90" : ""}`} />
                      </button>

                      {isOpen && (
                        <div className="px-4 pb-4">
                          <div className="rounded-lg bg-[#FBFCFE] border border-[#E4EAF4] p-3 space-y-2">
                            <div className="flex items-center justify-between">
                              <p className="text-[10px] font-extrabold uppercase tracking-[0.1em] text-[#9AA6BC]">Booking ID</p>
                              <p className="font-mono text-[11px] font-semibold text-[#3D4A66]">{b.id}</p>
                            </div>
                            {b.items.length === 0 ? (
                              <p className="text-xs text-[#6E7B96]">No ticket items recorded.</p>
                            ) : (
                              b.items.map((item, i) => (
                                <div key={i} className="flex items-center justify-between gap-3 border-t border-[#E4EAF4] pt-2 first:border-0 first:pt-0">
                                  <div className="min-w-0">
                                    <p className="truncate text-xs font-bold text-[#16224A]">
                                      {item.ticket_type_name || "Ticket"}
                                    </p>
                                    <p className="text-[11px] font-semibold text-[#6E7B96]">
                                      {item.quantity} &times; {fmtIDR(item.unit_price_rupiah)}
                                    </p>
                                  </div>
                                  <p className="text-xs font-extrabold tabular-nums text-[#16224A]">
                                    {fmtIDR(item.unit_price_rupiah * item.quantity)}
                                  </p>
                                </div>
                              ))
                            )}
                            {b.status === "pending" && (
                              <Link
                                href={`/checkout/${b.id}`}
                                className="btn-accent mt-1 flex w-full items-center justify-center gap-1.5 text-xs"
                              >
                                Continue checkout
                                <ArrowRight className="w-3 h-3" />
                              </Link>
                            )}
                          </div>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}

            {/* ── Payments table ── */}
            <div className="flex items-center justify-between gap-4 mb-4 mt-12">
              <p id="dash-payments" className="font-[family-name:var(--font-display)] text-xl text-[#16224A] scroll-mt-24">
                Payments
              </p>
              <p className="text-[11.5px] font-semibold text-[#9AA6BC]">Newest first</p>
            </div>

            {transactionsQuery.isLoading && (
              <div className="card text-center py-10">
                <Loader2 className="w-5 h-5 text-[#C9D4E8] animate-spin mx-auto mb-2" />
                <p className="text-[#6E7B96] text-sm">Loading payments...</p>
              </div>
            )}

            {!transactionsQuery.isLoading && transactions.length === 0 && (
              <div className="card text-center py-12">
                <CreditCard className="w-8 h-8 text-[#C9D4E8] mx-auto mb-3" />
                <p className="text-[#6E7B96] text-sm">No payment history yet</p>
              </div>
            )}

            {!transactionsQuery.isLoading && transactions.length > 0 && (
              <div className="rounded-xl border border-[#E4EAF4] overflow-hidden bg-white">
                <div className="grid grid-cols-[1fr_140px_120px] gap-3 items-center px-4 py-2.5 bg-[#F6F9FC] border-b border-[#E4EAF4]">
                  <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC]">Transaction</span>
                  <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC]">Date</span>
                  <span className="text-[9.5px] font-extrabold uppercase tracking-[0.11em] text-[#9AA6BC] text-right">Amount</span>
                </div>
                {transactions.map(t => (
                  <div key={t.id} className="grid grid-cols-[1fr_140px_120px] gap-3 items-center px-4 py-3.5 border-b border-[#E4EAF4] last:border-b-0">
                    <div className="min-w-0">
                      <p className="truncate font-mono text-[11.5px] font-semibold text-[#16224A]">{t.id}</p>
                      <span className={`badge mt-1 ${
                        t.status === "completed" ? "badge-green" :
                        t.status === "expired" ? "badge-red" :
                        t.status === "pending" || t.status === "initiated" ? "badge-yellow" : "badge-ink"
                      }`}>{t.status}</span>
                    </div>
                    <div className="text-[11.5px] font-semibold text-[#6E7B96]">
                      <LocalTime iso={t.created_at} format="short" />
                    </div>
                    <p className="text-right text-[13px] font-extrabold tabular-nums text-[#16224A]">
                      {fmtIDR(t.amount_rupiah)}
                    </p>
                  </div>
                ))}
              </div>
            )}
          </>
        )}
      </main>

      {/* Cancel event dialog */}
      <ConfirmDialog
        open={!!cancelling}
        title="Cancel Event"
        message={`Are you sure you want to cancel "${cancelling?.title}"? This action cannot be undone. All confirmed bookings will be cancelled and refunds issued.`}
        confirmLabel="Yes, Cancel"
        variant="danger"
        loading={false}
        onConfirm={() => {
          if (!cancelling) return;
          const cancelledID = cancelling.id;
          api.post<void>(`/api/events/${cancelledID}/cancel`)
            .then(() => {
              toast.success("Event cancelled");
              // Update the row before refetching: cancel cascades asynchronously,
              // so a plain invalidate briefly leaves the pre-cancel revenue
              // visible and Collect would flicker back.
              queryClient.setQueryData<{
                pages: OrganizerEventListResponse[];
                pageParams: unknown[];
              }>(["myEvents"], prev =>
                prev
                  ? {
                      ...prev,
                      pages: prev.pages.map(page => ({
                        ...page,
                        events: page.events.map(e =>
                          e.id === cancelledID
                            ? {
                                ...e,
                                status: "cancelled",
                                confirmed_revenue_rupiah: 0,
                                payout_status: "none" as const,
                              }
                            : e,
                        ),
                      })),
                    }
                  : prev,
              );
              queryClient.invalidateQueries({ queryKey: ["myEvents"] });
            })
            .catch((err: Error) => toast.error(err.message))
            .finally(() => setCancelling(null));
        }}
        onClose={() => setCancelling(null)}
      />

      {/* Reprocess cancellation dialog */}
      <ConfirmDialog
        open={!!reprocessing}
        title="Reprocess Cancellation"
        message={`Re-run the cancellation for "${reprocessing?.title}"? This will issue refunds for payments that completed after the cancellation and cancel any remaining pending bookings.`}
        confirmLabel="Yes, Reprocess"
        variant="warning"
        onConfirm={() => {
          if (!reprocessing) return;
          api.post<void>(`/api/events/${reprocessing.id}/cancel-reprocess`)
            .then(() => {
              toast.success("Cancellation reprocessed");
              queryClient.invalidateQueries({ queryKey: ["myEvents"] });
            })
            .catch((err: Error) => toast.error(err.message))
            .finally(() => setReprocessing(null));
        }}
        onClose={() => setReprocessing(null)}
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

      {accountDialogOpen && (
        <PayoutAccountDialog
          account={payoutAccount}
          loading={saveAccountMutation.isPending}
          onSubmit={(values) => saveAccountMutation.mutate(values)}
          onClose={() => setAccountDialogOpen(false)}
        />
      )}

      {payoutTarget && (
        <ConfirmDialog
          open
          title={
            payoutTarget.payout_status === "failed"
              ? "Retry Payout"
              : payoutTarget.payout_status === "paid_out"
                ? "Collect More"
                : "Request Payout"
          }
          message={`Send the completed, non-refunded earnings from "${payoutTarget.title}" to ${
            payoutAccount
              ? `${payoutAccount.bank_code} ••••${payoutAccount.bank_account_number.slice(-4)}`
              : "your saved payout account"
          }? The amount is calculated automatically and any earlier payout is kept.`}
          confirmLabel="Request Payout"
          variant="success"
          loading={requestPayoutMutation.isPending}
          onConfirm={() => requestPayoutMutation.mutate(payoutTarget.id)}
          onClose={() => setPayoutTarget(null)}
        />
      )}
    </div>
  );
}

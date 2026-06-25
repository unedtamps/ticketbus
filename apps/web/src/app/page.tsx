import Link from "next/link";
import { Calendar, MapPin } from "lucide-react";
import { fmtDate } from "@/lib/format";
import type { EventListResponse, EventItem } from "@/types";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8000";

function EventStub({ event, index }: { event: EventItem; index: number }) {
  return (
    <Link
      href={`/events/${event.id}`}
      className="card-stub fade-up group cursor-pointer flex flex-col justify-between min-h-[180px]"
      style={{ animationDelay: `${index * 0.07}s` }}
    >
      <div>
        <div className="flex items-start justify-between mb-2">
          <span
            className={`badge ${
              event.status === "published"
                ? "badge-green"
                : event.status === "cancelled"
                  ? "badge-red"
                  : "badge-yellow"
            }`}
          >
            {event.status}
          </span>
        </div>
        <h2 className="font-[family-name:var(--font-display)] text-lg text-[#1A1817] leading-snug mb-2 group-hover:text-[#D9381E] transition-colors">
          {event.title}
        </h2>
        {event.description ? (
          <p className="text-sm text-[#8B8580] line-clamp-2 mb-3">
            {event.description}
          </p>
        ) : null}
      </div>
      <div className="flex items-center gap-4 text-xs text-[#B0A89E] pt-3 border-t border-dashed border-[#E8E3DC]">
        <span className="flex items-center gap-1.5">
          <Calendar className="w-3.5 h-3.5" />
          {fmtDate(event.start_at)}
        </span>
        {event.venue_name ? (
          <span className="flex items-center gap-1.5">
            <MapPin className="w-3.5 h-3.5" />
            {event.venue_name}
          </span>
        ) : null}
      </div>
    </Link>
  );
}

export default async function HomePage() {
  let events: EventItem[] = [];

  try {
    const res = await fetch(`${API_URL}/api/events?limit=20`, {
      next: { revalidate: 60 },
    });
    if (res.ok) {
      const json = await res.json();
      events = json?.data?.events || [];
    }
  } catch {
    // Server-side fetch failed — render empty state
  }

  return (
    <div>
      {/* Hero */}
      <div className="text-center mb-14">
        <p className="font-[family-name:var(--font-display)] text-5xl md:text-6xl text-[#1A1817] leading-tight mb-4">
          Upcoming{" "}
          <span
            className="text-[#D9381E] inline-block"
            style={{ transform: "rotate(-1deg)" }}
          >
            Events
          </span>
        </p>
        <p className="text-[#8B8580] text-lg max-w-xl mx-auto font-[family-name:var(--font-body)]">
          Discover and book tickets for the best experiences around you
        </p>
      </div>

      {/* Empty */}
      {events.length === 0 && (
        <div className="text-center py-24">
          <div className="card-stub inline-block mx-auto mb-6 px-10 py-8">
            <p className="font-[family-name:var(--font-display)] text-2xl text-[#B0A89E] mb-2">
              No events yet
            </p>
            <p className="text-[#8B8580] text-sm">
              Check back soon for upcoming events
            </p>
          </div>
        </div>
      )}

      {/* Grid */}
      {events.length > 0 && (
        <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
          {events.map((event, i) => (
            <EventStub key={event.id} event={event} index={i} />
          ))}
        </div>
      )}
    </div>
  );
}

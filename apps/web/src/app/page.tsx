import Link from "next/link";
import { MapPin } from "lucide-react";
import { LocalTime } from "@/components/ui/local-time";
import { initialsOf } from "@/lib/format";
import type { EventListResponse, EventItem } from "@/types";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8000";

function EventCard({ event, index }: { event: EventItem; index: number }) {
  return (
    <Link
      href={`/events/${event.id}`}
      className="card fade-up group cursor-pointer overflow-hidden p-0 flex flex-col"
      style={{ animationDelay: `${index * 0.07}s` }}
    >
      {/* Poster */}
      <div className="relative h-[150px] w-full bg-[#F0F5FF]">
        {event.image_url ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={event.image_url}
            alt={event.title}
            className="absolute inset-0 w-full h-full object-cover"
          />
        ) : (
          <span className="absolute inset-0 flex items-center justify-center font-[family-name:var(--font-display)] text-3xl text-[#C9D4E8]">
            {initialsOf(event.title)}
          </span>
        )}
        <span
          className={`badge absolute top-2.5 left-2.5 ${
            event.status === "published"
              ? "badge-green"
              : event.status === "cancelled"
                ? "badge-red"
                : "badge-yellow"
          }`}
        >
          {event.status}
        </span>
        <span className="glass-chip absolute bottom-2.5 left-2.5">
          <LocalTime iso={event.start_at} format="date" />
        </span>
      </div>

      {/* Body */}
      <div className="p-4 flex flex-col gap-1.5">
        <h2 className="font-[family-name:var(--font-display)] text-[19px] text-[#16224A] leading-snug group-hover:text-[#2450D9] transition-colors line-clamp-1">
          {event.title}
        </h2>
        {event.venue_name ? (
          <p className="flex items-center gap-1.5 text-[13px] text-[#6E7B96]">
            <MapPin className="w-3.5 h-3.5 flex-shrink-0" />
            <span className="truncate">{event.venue_name}</span>
          </p>
        ) : null}
      </div>
    </Link>
  );
}

export default async function HomePage() {
  let events: EventItem[] = [];

  try {
    // No-store: a cached copy kept showing a stale list after an event went
    // live, which read as "the event did not save".
    const res = await fetch(`${API_URL}/api/events?limit=20`, {
      cache: "no-store",
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
        <p className="font-[family-name:var(--font-display)] text-5xl md:text-6xl text-[#16224A] leading-tight mb-4">
          Upcoming{" "}
          <span
            className="text-[#2450D9] inline-block"
            style={{ transform: "rotate(-1deg)" }}
          >
            Events
          </span>
        </p>
        <p className="text-[#6E7B96] text-lg max-w-xl mx-auto">
          Discover and book tickets for the best experiences around you
        </p>
      </div>

      {/* Empty */}
      {events.length === 0 && (
        <div className="text-center py-24">
          <div className="card inline-block mx-auto mb-6 px-10 py-8">
            <p className="font-[family-name:var(--font-display)] text-2xl text-[#9AA6BC] mb-2">
              No events yet
            </p>
            <p className="text-[#6E7B96] text-sm">
              Check back soon for live events
            </p>
          </div>
        </div>
      )}

      {/* Grid */}
      {events.length > 0 && (
        <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
          {events.map((event, i) => (
            <EventCard key={event.id} event={event} index={i} />
          ))}
        </div>
      )}
    </div>
  );
}

import EventPage from "./event-page";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8000";

export async function generateStaticParams() {
  try {
    const res = await fetch(`${API_URL}/api/events`);
    if (!res.ok) return [{ id: "fallback" }];
    const json = await res.json();
    const events: { id: string }[] = json.data || json || [];
    if (events.length === 0) return [{ id: "fallback" }];
    return events.map((e) => ({ id: e.id }));
  } catch {
    return [{ id: "fallback" }];
  }
}

export default function Page() {
  return <EventPage />;
}

import http from "k6/http";
import { check, sleep } from "k6";
import {
  randomItem,
  randomInt,
  authHeader,
  authAndJson,
  registerOrganizer,
  waitForOrganizer,
  createEvent,
  approveEvent,
  pollEventDetail,
  registerCustomer,
  login,
} from "./helpers.js";

export const options = {
  vus: 5,
  duration: "1m",
  thresholds: {
    http_req_duration: ["p(95)<500"],
    http_req_failed: ["rate<0.05"],
  },
};

const HOST = __ENV.TARGET_HOST || "http://localhost:8000";

export function setup() {
  const ts = Date.now();

  // 1. Register EO + wait for Kafka
  const eoEmail = `eo-smoke-${ts}@test.com`;
  const eo = registerOrganizer(HOST, eoEmail, "Smoke EO");
  waitForOrganizer(HOST, eo.token);

  // 2. Create 2 events
  const eventIDs = [];
  for (let i = 0; i < 2; i++) {
    const id = createEvent(HOST, eo.token, `Smoke Event ${i} ${ts}`, 200);
    if (id) eventIDs.push(id);
  }

  // 3. Admin login + approve all events
  const admin = login(HOST, "admin@test.com", "admin123");
  for (const id of eventIDs) {
    approveEvent(HOST, admin.token, id);
  }

  // 4. Register a customer for polling event detail
  const pollCust = registerCustomer(HOST);

  // 5. Poll each event until seats are initialized → collect ticket_type_ids
  const events = [];
  for (const id of eventIDs) {
    const tts = pollEventDetail(HOST, pollCust.token, id);
    for (const tt of tts) {
      events.push({ eventID: id, ttID: tt.id, ttName: tt.name, priceCents: tt.priceCents });
    }
  }

  // 6. Register 5 customers
  const customers = [];
  for (let i = 0; i < 5; i++) {
    customers.push(registerCustomer(HOST));
  }

  return { events, customers };
}

export default function (data) {
  const cust = randomItem(data.customers);
  const roll = randomInt(1, 100);

  if (roll <= 60) {
    // 60% — browse public events
    const res = http.get(`${HOST}/api/events`);
    check(res, {
      "browse: 200": (r) => r.status === 200,
      "browse: events array": (r) =>
        Array.isArray(JSON.parse(r.body).data?.events),
    });
  } else if (roll <= 75) {
    // 15% — event detail
    const ev = randomItem(data.events);
    const res = http.get(
      `${HOST}/api/events/${ev.eventID}`,
      authHeader(cust.token),
    );
    check(res, {
      "detail: 200": (r) => r.status === 200,
      "detail: has ticket_types": (r) => {
        const b = JSON.parse(r.body);
        return (
          Array.isArray(b.data?.ticket_types) && b.data.ticket_types.length > 0
        );
      },
    });
  } else if (roll <= 85) {
    // 10% — auth/me
    const res = http.get(`${HOST}/api/auth/me`, authHeader(cust.token));
    check(res, {
      "me: 200": (r) => r.status === 200,
      "me: has role": (r) =>
        typeof JSON.parse(r.body).data?.user?.role === "string",
    });
  } else {
    // 15% — reserve
    const ev = randomItem(data.events);
    const qty = randomInt(1, 2);
    const res = http.post(
      `${HOST}/api/inventory/reserve`,
      JSON.stringify({
        event_id: ev.eventID,
        items: [
           { ticket_type_id: ev.ttID, quantity: qty, unit_price_cents: ev.priceCents },
        ],
      }),
      authAndJson(cust.token),
    );
    check(res, {
      "reserve: success": (r) => r.status === 200 || r.status === 201,
      "reserve: booking_id": (r) => {
        if (r.status !== 200 && r.status !== 201) return true; // skip on failure
        return typeof JSON.parse(r.body).data?.booking_id === "string";
      },
    });
  }

  sleep(randomInt(1, 3));
}

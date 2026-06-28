import http from "k6/http";
import { check, sleep } from "k6";
import {
  randomItem,
  randomInt,
  authHeader,
  authAndJson,
  registerOrganizer,
  waitForOrganizer,
  approveEvent,
  pollEventDetail,
  registerCustomer,
} from "./helpers.js";

export const options = {
  stages: [
    { duration: "1m", target: 50 },
    { duration: "3m", target: 50 },
    { duration: "1m", target: 0 },
  ],
  thresholds: {
    http_req_duration: ["p(95)<1000"],
    http_req_failed: ["rate<0.2"],
  },
};

const HOST = __ENV.TARGET_HOST || "http://localhost:8000";

export function setup() {
  const ts = Date.now();

  const eoEmail = `eo-load-${ts}@test.com`;
  const eo = registerOrganizer(HOST, eoEmail, "Load EO");
  waitForOrganizer(HOST, eo.token);

  const eventIDs = [];
  const future = new Date(Date.now() + 30 * 86400000).toISOString();
  const futureEnd = new Date(
    Date.now() + 30 * 86400000 + 3 * 3600000,
  ).toISOString();
  for (let i = 0; i < 5; i++) {
    const res = http.post(
      `${HOST}/api/events`,
      JSON.stringify({
        title: `Load Event ${i} ${ts}`,
        description: "k6 load test",
        venue_name: "K6 Stadium",
        venue_address: "123 K6 St",
        venue_capacity: 300,
        start_at: future,
        end_at: futureEnd,
        ticket_types: [
          { name: "GA", price_cents: 5000, quantity: 200, max_per_order: 10 },
          { name: "VIP", price_cents: 10000, quantity: 100, max_per_order: 5 },
        ],
      }),
      authAndJson(eo.token),
    );
    if (res.status === 200 || res.status === 201) {
      eventIDs.push(JSON.parse(res.body).data.id);
    }
  }

  const admin = http.post(
    `${HOST}/api/auth/login`,
    JSON.stringify({ email: "admin@test.com", password: "admin123" }),
    { headers: { "Content-Type": "application/json" } },
  );
  const adminToken = JSON.parse(admin.body).data.access_token;
  for (const id of eventIDs) {
    approveEvent(HOST, adminToken, id);
  }

  const pollCust = registerCustomer(HOST);
  const events = [];
  for (const id of eventIDs) {
    const tts = pollEventDetail(HOST, pollCust.token, id);
    for (const tt of tts) {
      events.push({ eventID: id, ttID: tt.id, ttName: tt.name, priceCents: tt.priceCents });
    }
  }

  const customers = [];
  for (let i = 0; i < 20; i++) {
    customers.push(registerCustomer(HOST));
  }

  return { events, customers };
}

export default function (data) {
  const cust = randomItem(data.customers);
  const roll = randomInt(1, 100);

  if (roll <= 50) {
    const res = http.get(`${HOST}/api/events`);
    check(res, {
      "browse: 200": (r) => r.status === 200,
      "browse: array": (r) => Array.isArray(JSON.parse(r.body).data?.events),
    });
  } else if (roll <= 65) {
    const ev = randomItem(data.events);
    const res = http.get(
      `${HOST}/api/events/${ev.eventID}`,
      authHeader(cust.token),
    );
    check(res, {
      "detail: 200": (r) => r.status === 200,
      "detail: ticket_types": (r) =>
        Array.isArray(JSON.parse(r.body).data?.ticket_types),
    });
  } else if (roll <= 75) {
    const res = http.get(`${HOST}/api/auth/me`, authHeader(cust.token));
    check(res, { "me: 200": (r) => r.status === 200 });
  } else {
    const ev = randomItem(data.events);
    const qty = randomInt(1, 2);
    const res = http.post(
      `${HOST}/api/inventory/reserve`,
      JSON.stringify({
        event_id: ev.eventID,
        items: [
          {
            ticket_type_id: ev.ttID,
            quantity: qty,
            unit_price_cents: ev.priceCents,
          },
        ],
      }),
      authAndJson(cust.token),
    );
    check(res, {
      "reserve: ok": (r) =>
        r.status === 200 || r.status === 201 || r.status === 409,
    });
  }

  sleep(1);
}

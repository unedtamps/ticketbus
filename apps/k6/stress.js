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
    { duration: "2m", target: 100 },
    { duration: "3m", target: 200 },
    { duration: "3m", target: 300 },
    { duration: "2m", target: 0 },
  ],
  thresholds: {
    http_req_duration: ["p(95)<3000"],
    http_req_failed: ["rate<0.2"],
  },
};

const HOST = __ENV.TARGET_HOST || "http://localhost:8000";

export function setup() {
  const ts = Date.now();

  // 1. Register EO + wait
  const eoEmail = `eo-stress-${ts}@test.com`;
  const eo = registerOrganizer(HOST, eoEmail, "Stress EO");
  waitForOrganizer(HOST, eo.token);

  // 2. Create 5 events (500 seats each)
  const eventIDs = [];
  for (let i = 0; i < 5; i++) {
    const future = new Date(Date.now() + 30 * 86400000).toISOString();
    const futureEnd = new Date(
      Date.now() + 30 * 86400000 + 3 * 3600000,
    ).toISOString();
    const res = http.post(
      `${HOST}/api/events`,
      JSON.stringify({
        title: `Stress Event ${i} ${ts}`,
        description: "k6 stress test",
        venue_name: "K6 Arena",
        venue_address: "456 K6 Blvd",
        venue_capacity: 500,
        start_at: future,
        end_at: futureEnd,
        ticket_types: [
          { name: "GA", price_cents: 2500, quantity: 500, max_per_order: 10 },
        ],
      }),
      authAndJson(eo.token),
    );
    if (res.status === 200 || res.status === 201) {
      eventIDs.push(JSON.parse(res.body).data.id);
    }
  }

  // 3. Admin login + approve
  const admin = http.post(
    `${HOST}/api/auth/login`,
    JSON.stringify({ email: "admin@test.com", password: "admin123" }),
    { headers: { "Content-Type": "application/json" } },
  );
  const adminToken = JSON.parse(admin.body).data.access_token;
  for (const id of eventIDs) {
    approveEvent(HOST, adminToken, id);
  }

  // 4. Poll events for ticket_type_ids
  const pollCust = registerCustomer(HOST);
  const events = [];
  for (const id of eventIDs) {
    const tts = pollEventDetail(HOST, pollCust.token, id);
    for (const tt of tts) {
      events.push({ eventID: id, ttID: tt.id, ttName: tt.name, priceCents: tt.priceCents });
    }
  }

  // 5. Register 50 customers
  const customers = [];
  for (let i = 0; i < 50; i++) {
    customers.push(registerCustomer(HOST));
  }

  return { events, customers };
}

export default function (data) {
  const cust = randomItem(data.customers);
  const roll = randomInt(1, 100);

  if (roll <= 45) {
    const res = http.get(`${HOST}/api/events`);
    check(res, {
      "browse: 200": (r) => r.status === 200,
      "browse: array": (r) => Array.isArray(JSON.parse(r.body).data?.events),
    });
  } else if (roll <= 60) {
    const ev = randomItem(data.events);
    const res = http.get(
      `${HOST}/api/events/${ev.eventID}`,
      authHeader(cust.token),
    );
    check(res, {
      "detail: 200": (r) => r.status === 200,
    });
  } else if (roll <= 70) {
    const res = http.get(`${HOST}/api/auth/me`, authHeader(cust.token));
    check(res, { "me: 200": (r) => r.status === 200 });
  } else {
    const ev = randomItem(data.events);
    const res = http.post(
      `${HOST}/api/inventory/reserve`,
      JSON.stringify({
        event_id: ev.eventID,
        items: [
          {
            ticket_type_id: ev.ttID,
            quantity: randomInt(1, 3),
            unit_price_cents: ev.priceCents,
          },
        ],
      }),
      authAndJson(cust.token),
    );
    check(res, {
      "reserve: status": (r) =>
        r.status === 200 || r.status === 201 || r.status === 409,
    });
  }

  sleep(randomInt(500, 1500) / 1000);
}

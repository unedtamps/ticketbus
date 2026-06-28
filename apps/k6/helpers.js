import http from "k6/http";
import { sleep } from "k6";

export function randomItem(arr) {
  return arr[Math.floor(Math.random() * arr.length)];
}

export function randomInt(min, max) {
  return Math.floor(Math.random() * (max - min + 1)) + min;
}

export function authHeader(token) {
  return { headers: { Authorization: `Bearer ${token}` } };
}

export function jsonBody(obj) {
  return { headers: { "Content-Type": "application/json" } };
}

export function authAndJson(token) {
  return {
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
    },
  };
}

export function login(baseURL, email, password) {
  const res = http.post(
    `${baseURL}/api/auth/login`,
    JSON.stringify({ email, password }),
    jsonBody({}),
  );
  if (res.status !== 200) {
    throw new Error(`login ${email}: HTTP ${res.status} — ${res.body.substring(0, 200)}`);
  }
  const body = JSON.parse(res.body);
  return { token: body.data.access_token, userID: body.data.user?.id };
}

export function registerCustomer(baseURL) {
  const email = `cust-${Date.now()}-${Math.floor(Math.random() * 100000)}@test.com`;
  const res = http.post(
    `${baseURL}/api/auth/register`,
    JSON.stringify({ name: "K6 User", email, password: "test1234", role: "customer" }),
    jsonBody({}),
  );
  if (res.status !== 200 && res.status !== 201) {
    throw new Error(`register ${email}: HTTP ${res.status} — ${res.body.substring(0, 200)}`);
  }
  return login(baseURL, email, "test1234");
}

export function registerOrganizer(baseURL, email, name) {
  const res = http.post(
    `${baseURL}/api/auth/register/organizer`,
    JSON.stringify({
      name,
      email,
      password: "test1234",
      organizer_name: "K6 Organizer",
      contact_email: email,
    }),
    jsonBody({}),
  );
  if (res.status !== 200 && res.status !== 201) {
    throw new Error(`register EO ${email}: HTTP ${res.status} — ${res.body.substring(0, 200)}`);
  }
  return login(baseURL, email, "test1234");
}

export function waitForOrganizer(baseURL, token) {
  for (let i = 0; i < 30; i++) {
    if (i > 0) sleep(1);
    const r = http.get(`${baseURL}/api/events/organizers/me`, authHeader(token));
    if (r.status === 200) return;
  }
}

export function createEvent(baseURL, token, name, qty) {
  const future = new Date(Date.now() + 30 * 86400000).toISOString();
  const futureEnd = new Date(Date.now() + 30 * 86400000 + 3 * 3600000).toISOString();
  const res = http.post(
    `${baseURL}/api/events`,
    JSON.stringify({
      title: name,
      description: "k6 test event",
      venue_name: "K6 Stadium",
      venue_address: "123 K6 St",
      venue_capacity: qty,
      start_at: future,
      end_at: futureEnd,
      ticket_types: [
        { name: "GA", price_cents: 5000, quantity: qty, max_per_order: 10 },
      ],
    }),
    authAndJson(token),
  );
  if (res.status !== 200 && res.status !== 201) return null;
  return JSON.parse(res.body).data.id;
}

export function approveEvent(baseURL, adminToken, eventID) {
  return http.post(`${baseURL}/api/events/${eventID}/approve`, null, authHeader(adminToken));
}

export function pollEventDetail(baseURL, token, eventID) {
  for (let i = 0; i < 30; i++) {
    if (i > 0) sleep(1);
    const r = http.get(`${baseURL}/api/events/${eventID}`, authHeader(token));
    if (r.status !== 200) continue;
    const body = JSON.parse(r.body);
    if (body.data?.ticket_types?.length > 0 && body.data.ticket_types[0].available > 0) {
      return body.data.ticket_types.map((tt) => ({
        id: tt.id,
        name: tt.name,
        available: tt.available,
        priceCents: tt.price_cents,
      }));
    }
  }
  return [];
}

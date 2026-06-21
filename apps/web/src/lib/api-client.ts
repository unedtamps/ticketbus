const BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8000";

import { toast } from "@/components/ui/toast";

class ApiClient {
  private refreshPromise: Promise<boolean> | null = null;

  private getToken(): string | null {
    if (typeof window === "undefined") return null;
    return localStorage.getItem("access_token");
  }

  private getRefreshToken(): string | null {
    if (typeof window === "undefined") return null;
    return localStorage.getItem("refresh_token");
  }

  private async tryRefresh(): Promise<boolean> {
    // Deduplicate concurrent refresh attempts
    if (this.refreshPromise) return this.refreshPromise;
    this.refreshPromise = this._doRefresh();
    const result = await this.refreshPromise;
    this.refreshPromise = null;
    return result;
  }

  private async _doRefresh(): Promise<boolean> {
    const rt = this.getRefreshToken();
    if (!rt) return false;
    try {
      const res = await fetch(`${BASE_URL}/api/auth/refresh`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refresh_token: rt }),
      });
      if (!res.ok) return false;
      const json = await this.readJSON(res);
      if (!json) return false;
      const data = (json.data as Record<string, unknown>) || json;
      if (data.access_token) {
        localStorage.setItem("access_token", data.access_token as string);
        if (data.refresh_token) localStorage.setItem("refresh_token", data.refresh_token as string);
        return true;
      }
      return false;
    } catch {
      return false;
    }
  }

  private clearSession() {
    localStorage.removeItem("access_token");
    localStorage.removeItem("refresh_token");
    window.dispatchEvent(new CustomEvent("auth:expired"));
  }

  private async readJSON(res: Response): Promise<Record<string, unknown> | null> {
    const contentType = res.headers.get("content-type") || "";
    if (!contentType.includes("application/json")) {
      const text = await res.text().catch(() => "");
      toast.error("Server returned an unexpected response. Check console for details.");
      console.error(`Non-JSON response from ${res.url}:`, text.slice(0, 500));
      return null;
    }
    try {
      return await res.json();
    } catch {
      return null;
    }
  }

  private async request<T>(path: string, options: RequestInit = {}, retry = true): Promise<T> {
    const token = this.getToken();
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      ...(options.headers as Record<string, string>),
    };
    if (token) {
      headers["Authorization"] = `Bearer ${token}`;
    }
    const res = await fetch(`${BASE_URL}${path}`, { ...options, headers });

    const json = await this.readJSON(res);

    // On 401, attempt token refresh and retry once
    if (res.status === 401 && retry && this.getRefreshToken()) {
      const refreshed = await this.tryRefresh();
      if (refreshed) {
        return this.request<T>(path, options, false);
      }
      this.clearSession();
      toast.info("Session expired. Please log in again.");
      throw new Error("Session expired. Please login again.");
    }

    if (!res.ok) {
      const errMsg = json?.error || json?.message || `Server error (${res.status})`;
      throw new Error(String(errMsg));
    }
    return json?.data !== undefined ? (json.data as T) : (json as unknown as T);
  }

  get<T>(path: string) { return this.request<T>(path); }
  post<T>(path: string, body?: unknown) { return this.request<T>(path, { method: "POST", body: body ? JSON.stringify(body) : undefined }); }
  put<T>(path: string, body?: unknown) { return this.request<T>(path, { method: "PUT", body: body ? JSON.stringify(body) : undefined }); }
  delete<T>(path: string) { return this.request<T>(path, { method: "DELETE" }); }
}

export const api = new ApiClient();

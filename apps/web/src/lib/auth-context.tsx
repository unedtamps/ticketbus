"use client";

import React, { createContext, useContext, useEffect, useState, useCallback } from "react";
import { useRouter } from "next/navigation";
import { api } from "./api-client";
import { toast } from "@/components/ui/toast";
import type { User } from "@/types";

interface OrganizerFields {
  organizer_name: string;
  description?: string;
  profile_link?: string;
  contact_email: string;
}

interface AuthState {
  user: User | null;
  accessToken: string | null;
  loading: boolean;
  hydrated: boolean;
  isAdmin: boolean;
  isEO: boolean;
  isCustomer: boolean;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string, name: string, role?: string, organizerFields?: OrganizerFields) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthState>({
  user: null, accessToken: null, loading: true, hydrated: false,
  isAdmin: false, isEO: false, isCustomer: false,
  login: async () => {}, register: async () => {}, logout: () => {},
});

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [hydrated, setHydrated] = useState(false);
  const router = useRouter();

  useEffect(() => { setHydrated(true); }, []);

  const logout = useCallback(() => {
    localStorage.removeItem("access_token");
    localStorage.removeItem("refresh_token");
    setUser(null);
    setToken(null);
    router.push("/login");
  }, [router]);

  // Restore session on mount
  useEffect(() => {
    const saved = localStorage.getItem("access_token");
    if (!saved) {
      setLoading(false);
      return;
    }
    setToken(saved);
    api.get<{ user: User }>("/api/auth/me")
      .then(data => setUser(data.user))
      .catch(() => logout())
      .finally(() => setLoading(false));
  }, [logout]);

  // Listen for auth:expired events from api-client
  useEffect(() => {
    const handler = () => {
      toast.info("Session expired. Please log in again.");
      setUser(null);
      setToken(null);
      router.push("/login");
    };
    window.addEventListener("auth:expired", handler);
    return () => window.removeEventListener("auth:expired", handler);
  }, [router]);

  async function login(email: string, password: string) {
    const data = await api.post<{ access_token: string; expires_in: number; refresh_token?: string; user?: User }>(
      "/api/auth/login", { email, password }
    );
    localStorage.setItem("access_token", data.access_token);
    if (data.refresh_token) localStorage.setItem("refresh_token", data.refresh_token);
    setToken(data.access_token);
    if (data.user) {
      setUser(data.user);
    } else {
      const me = await api.get<{ user: User }>("/api/auth/me");
      setUser(me.user);
    }
  }

  async function register(email: string, password: string, name: string, role?: string, organizerFields?: OrganizerFields) {
    if (role === "eo" && organizerFields) {
      await api.post("/api/auth/register/organizer", {
        email, password, name,
        organizer_name: organizerFields.organizer_name,
        description: organizerFields.description || "",
        profile_link: organizerFields.profile_link || "",
        contact_email: organizerFields.contact_email,
      });
    } else {
      await api.post("/api/auth/register", { email, password, name, role });
    }
  }

  const value: AuthState = {
    user,
    accessToken: token,
    loading,
    hydrated,
    isAdmin: user?.role === "admin",
    isEO: user?.role === "eo",
    isCustomer: user?.role === "customer",
    login,
    register,
    logout,
  };

  return (
    <AuthContext.Provider value={value}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  return useContext(AuthContext);
}

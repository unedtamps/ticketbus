"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import { LogIn } from "lucide-react";

export default function LoginPage() {
  const { login } = useAuth();
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setLoading(true);
    try {
      await login(email, password);
      router.push("/dashboard");
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : "Login failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="min-h-[80vh] flex items-center justify-center">
      <div className="w-full max-w-sm">
        <div className="text-center mb-8">
          <p className="font-[family-name:var(--font-display)] text-3xl text-[#1A1817] mb-2">Admit One</p>
          <p className="text-[#8B8580] text-sm">Sign in to your account</p>
        </div>

        <form onSubmit={handleSubmit} className="card space-y-4">
          <div>
            <label className="block text-sm font-medium text-[#4A4541] mb-1.5">Email</label>
            <input
              type="email" required value={email}
              onChange={e => setEmail(e.target.value)}
              className="input-field" placeholder="you@example.com"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-[#4A4541] mb-1.5">Password</label>
            <input
              type="password" required value={password}
              onChange={e => setPassword(e.target.value)}
              className="input-field" placeholder="••••••••"
            />
          </div>

          <button type="submit" disabled={loading} className="btn-accent w-full">
            <LogIn className="w-4 h-4" />
            {loading ? "Signing in..." : "Sign in"}
          </button>

          <p className="text-center text-sm text-[#8B8580]">
            Don&apos;t have an account?{" "}
            <Link href="/register" className="text-[#D9381E] hover:underline font-medium">
              Register
            </Link>
          </p>
        </form>
      </div>
    </div>
  );
}

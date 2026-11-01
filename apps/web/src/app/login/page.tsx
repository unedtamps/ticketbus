"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import { AuthField, AuthShell, AuthWordmark } from "@/components/auth/auth-shell";
import {
  ArrowRight,
  CreditCard,
  Eye,
  EyeOff,
  Lock,
  Mail,
  ShieldCheck,
} from "lucide-react";

export default function LoginPage() {
  const { login } = useAuth();
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
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
    <AuthShell>
      <AuthWordmark />

      <h1 className="mt-6 font-[family-name:var(--font-display)] text-4xl leading-[1.1] text-[#16224A]">
        Masuk ke akunmu
      </h1>
      <p className="mt-2.5 text-sm leading-relaxed text-[#6E7B96]">
        Belum punya akun?{" "}
        <Link href="/register" className="font-bold text-[#2450D9] hover:underline">
          Daftar gratis
        </Link>{" "}
        sebagai pembeli atau organizer.
      </p>

      <form onSubmit={handleSubmit} className="mt-7 space-y-4">
        <AuthField
          id="email"
          label="Email"
          type="email"
          value={email}
          onChange={setEmail}
          placeholder="arka@mail.com"
          icon={Mail}
          required
          autoComplete="email"
        />

        <AuthField
          id="password"
          label="Kata sandi"
          type={showPassword ? "text" : "password"}
          value={password}
          onChange={setPassword}
          placeholder="••••••••••"
          icon={Lock}
          required
          autoComplete="current-password"
          trailing={
            <button
              type="button"
              onClick={() => setShowPassword(v => !v)}
              aria-label={showPassword ? "Sembunyikan kata sandi" : "Tampilkan kata sandi"}
              className="flex-shrink-0 text-[#9AA6BC] transition-colors hover:text-[#16224A]"
            >
              {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </button>
          }
        />

        <button
          type="submit"
          disabled={loading}
          className="flex w-full items-center justify-center gap-2 rounded-xl bg-[#2450D9] px-5 py-3.5 text-sm font-extrabold text-white transition-colors duration-150 hover:bg-[#1D3FAE] disabled:opacity-60"
        >
          <ArrowRight className="h-4 w-4" />
          {loading ? "Memproses..." : "Masuk"}
        </button>

        <div className="flex items-center gap-3 py-1">
          <span className="h-px flex-1 bg-[#E4EAF4]" />
          <span className="text-xs text-[#9AA6BC]">atau</span>
          <span className="h-px flex-1 bg-[#E4EAF4]" />
        </div>

        <Link
          href="/register"
          className="flex w-full items-center justify-center rounded-xl border border-[#E4EAF4] bg-white px-5 py-3 text-sm font-bold text-[#16224A] transition-colors duration-150 hover:border-[#C9D4E8] hover:bg-[#F6F9FC]"
        >
          Daftar
        </Link>
      </form>

      <p className="mt-8 text-xs leading-relaxed text-[#9AA6BC]">
        Dengan masuk, kamu setuju pada Syarat &amp; Ketentuan Ticketbus.
      </p>
      <div className="mt-4 flex items-center gap-5 text-xs font-semibold text-[#9AA6BC]">
        <span className="flex items-center gap-1.5">
          <ShieldCheck className="h-3.5 w-3.5" />
          Tiket resmi
        </span>
        <span className="flex items-center gap-1.5">
          <CreditCard className="h-3.5 w-3.5" />
          Pembayaran aman
        </span>
      </div>
    </AuthShell>
  );
}

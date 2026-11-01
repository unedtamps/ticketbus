"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import { AuthField, AuthShell, AuthWordmark } from "@/components/auth/auth-shell";
import { CalendarPlus, Lock, Mail, Ticket, User, UserPlus } from "lucide-react";

// Role is a pair of cards rather than a <select>: it is the first decision on
// this page and picking organizer reveals a whole extra section, so it should
// be visible and explain itself.
function RoleCard({
  active,
  icon: Icon,
  title,
  description,
  onClick,
}: {
  active: boolean;
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  description: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`flex-1 rounded-xl border p-3.5 text-left transition-colors duration-150 ${
        active
          ? "border-[#2450D9] bg-[#F0F5FF]"
          : "border-[#E4EAF4] bg-white hover:border-[#C9D4E8] hover:bg-[#F6F9FC]"
      }`}
    >
      <span className="flex items-center gap-2">
        <Icon className={`h-4 w-4 ${active ? "text-[#2450D9]" : "text-[#9AA6BC]"}`} />
        <span
          className={`text-[13.5px] font-extrabold ${
            active ? "text-[#2450D9]" : "text-[#16224A]"
          }`}
        >
          {title}
        </span>
      </span>
      <span className="mt-1 block text-[11.5px] leading-snug text-[#6E7B96]">
        {description}
      </span>
    </button>
  );
}

export default function RegisterPage() {
  const { register } = useAuth();
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [role, setRole] = useState("customer");
  const [loading, setLoading] = useState(false);

  const [organizerName, setOrganizerName] = useState("");
  const [description, setDescription] = useState("");
  const [profileLink, setProfileLink] = useState("");
  const [contactEmail, setContactEmail] = useState("");

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setLoading(true);
    try {
      if (role === "eo") {
        await register(email, password, name, role, {
          organizer_name: organizerName,
          description,
          profile_link: profileLink,
          contact_email: contactEmail,
        });
      } else {
        await register(email, password, name, role);
      }
      router.push("/login");
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : "Registration failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <AuthShell>
      <AuthWordmark />

      <h1 className="mt-6 font-[family-name:var(--font-display)] text-4xl leading-[1.1] text-[#16224A]">
        Buat akunmu
      </h1>
      <p className="mt-2.5 text-sm leading-relaxed text-[#6E7B96]">
        Gratis untuk pembeli dan organizer. Butuh kurang dari satu menit.
      </p>

      <form onSubmit={handleSubmit} className="mt-6 space-y-4">
        <div>
          <p className="mb-2 text-[13px] font-semibold text-[#16224A]">
            Saya mendaftar sebagai
          </p>
          <div className="flex gap-2.5">
            <RoleCard
              active={role === "customer"}
              icon={Ticket}
              title="Pembeli"
              description="Beli tiket & kelola pesanan"
              onClick={() => setRole("customer")}
            />
            <RoleCard
              active={role === "eo"}
              icon={CalendarPlus}
              title="Organizer"
              description="Buat & jual tiket event"
              onClick={() => setRole("eo")}
            />
          </div>
        </div>

        <AuthField
          id="name"
          label="Nama lengkap"
          value={name}
          onChange={setName}
          placeholder="Rani Prasetyo"
          icon={User}
          required
          autoComplete="name"
        />
        <AuthField
          id="email"
          label="Email"
          type="email"
          value={email}
          onChange={setEmail}
          placeholder="rani@mail.com"
          icon={Mail}
          required
          autoComplete="email"
        />
        <AuthField
          id="password"
          label="Kata sandi"
          type="password"
          value={password}
          onChange={setPassword}
          placeholder="Minimal 6 karakter"
          icon={Lock}
          required
          minLength={6}
          autoComplete="new-password"
        />

        {/* Organizer profile only matters for EO, so it stays collapsed until
            that role is chosen. */}
        {role === "eo" && (
          <div className="space-y-3 rounded-xl border border-dashed border-[#E4EAF4] bg-[#FBFCFE] p-4">
            <p className="text-[10.5px] font-extrabold uppercase tracking-[0.1em] text-[#9AA6BC]">
              Profil Organizer
            </p>
            <AuthField
              id="organizer_name"
              label="Nama organisasi"
              value={organizerName}
              onChange={setOrganizerName}
              placeholder="Acme Events"
              icon={User}
              required
            />
            <div>
              <label
                htmlFor="description"
                className="mb-1.5 block text-[13px] font-semibold text-[#16224A]"
              >
                Deskripsi
              </label>
              <textarea
                id="description"
                value={description}
                onChange={e => setDescription(e.target.value)}
                rows={2}
                placeholder="Ceritakan singkat tentang event-mu"
                className="w-full rounded-xl border border-[#E4EAF4] bg-white px-3.5 py-3 text-sm text-[#16224A] outline-none transition-colors placeholder:text-[#9AA6BC] focus:border-[#2450D9] focus:shadow-[0_0_0_3px_rgba(36,80,217,0.08)]"
              />
            </div>
            <AuthField
              id="profile_link"
              label="Link profil"
              type="url"
              value={profileLink}
              onChange={setProfileLink}
              placeholder="https://instagram.com/acme"
              icon={User}
            />
            <AuthField
              id="contact_email"
              label="Email kontak"
              type="email"
              value={contactEmail}
              onChange={setContactEmail}
              placeholder="halo@acme.com"
              icon={Mail}
              required
            />
          </div>
        )}

        <button
          type="submit"
          disabled={loading}
          className="flex w-full items-center justify-center gap-2 rounded-xl bg-[#2450D9] px-5 py-3.5 text-sm font-extrabold text-white transition-colors duration-150 hover:bg-[#1D3FAE] disabled:opacity-60"
        >
          <UserPlus className="h-4 w-4" />
          {loading ? "Membuat akun..." : "Buat akun"}
        </button>
      </form>

      <p className="mt-6 text-center text-[13px] text-[#6E7B96]">
        Sudah punya akun?{" "}
        <Link href="/login" className="font-extrabold text-[#2450D9] hover:underline">
          Masuk
        </Link>
      </p>
    </AuthShell>
  );
}

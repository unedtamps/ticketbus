"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth-context";
import { toast } from "@/components/ui/toast";
import { UserPlus } from "lucide-react";

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
          organizer_name: organizerName, description,
          profile_link: profileLink, contact_email: contactEmail,
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
    <div className="min-h-[80vh] flex items-center justify-center py-8">
      <div className="w-full max-w-sm">
        <div className="text-center mb-8">
          <p className="font-[family-name:var(--font-display)] text-3xl text-[#1A1817] mb-2">Join the audience</p>
          <p className="text-[#8B8580] text-sm">Create your TicketSaas account</p>
        </div>

        <form onSubmit={handleSubmit} className="card space-y-4">
          <div>
            <label className="block text-sm font-medium text-[#4A4541] mb-1.5">Name</label>
            <input type="text" required value={name} onChange={e => setName(e.target.value)}
              className="input-field" placeholder="John Doe" />
          </div>

          <div>
            <label className="block text-sm font-medium text-[#4A4541] mb-1.5">Email</label>
            <input type="email" required value={email} onChange={e => setEmail(e.target.value)}
              className="input-field" placeholder="you@example.com" />
          </div>

          <div>
            <label className="block text-sm font-medium text-[#4A4541] mb-1.5">Password</label>
            <input type="password" required value={password} onChange={e => setPassword(e.target.value)}
              className="input-field" placeholder="••••••••" minLength={6} />
          </div>

          <div>
            <label className="block text-sm font-medium text-[#4A4541] mb-1.5">Role</label>
            <select value={role} onChange={e => setRole(e.target.value)}
              className="input-field">
              <option value="customer">Customer — buy tickets</option>
              <option value="eo">Event Organizer — create events</option>
            </select>
          </div>

          {role === "eo" && (
            <div className="space-y-3 pt-3 border-t border-dashed border-[#E8E3DC]">
              <p className="text-xs font-semibold text-[#B0A89E] uppercase tracking-wider">Organizer Profile</p>
              <div>
                <label className="block text-sm font-medium text-[#4A4541] mb-1.5">Organization Name</label>
                <input type="text" required value={organizerName} onChange={e => setOrganizerName(e.target.value)}
                  className="input-field" placeholder="Acme Events" />
              </div>
              <div>
                <label className="block text-sm font-medium text-[#4A4541] mb-1.5">Description</label>
                <textarea value={description} onChange={e => setDescription(e.target.value)}
                  className="input-field" rows={2} placeholder="Brief description" />
              </div>
              <div>
                <label className="block text-sm font-medium text-[#4A4541] mb-1.5">Profile Link</label>
                <input type="url" value={profileLink} onChange={e => setProfileLink(e.target.value)}
                  className="input-field" placeholder="https://..." />
              </div>
              <div>
                <label className="block text-sm font-medium text-[#4A4541] mb-1.5">Contact Email</label>
                <input type="email" required value={contactEmail} onChange={e => setContactEmail(e.target.value)}
                  className="input-field" placeholder="contact@org.com" />
              </div>
            </div>
          )}

          <button type="submit" disabled={loading} className="btn-accent w-full">
            <UserPlus className="w-4 h-4" />
            {loading ? "Creating..." : "Create account"}
          </button>

          <p className="text-center text-sm text-[#8B8580]">
            Already registered?{" "}
            <Link href="/login" className="text-[#D9381E] hover:underline font-medium">Sign in</Link>
          </p>
        </form>
      </div>
    </div>
  );
}

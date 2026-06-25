import Link from "next/link";
import { Ticket } from "lucide-react";

export default function NotFound() {
  return (
    <div className="flex items-center justify-center min-h-[60vh] fade-up">
      <div className="card-stub text-center max-w-md mx-auto py-12 px-8 space-y-5">
        <div className="w-14 h-14 rounded-xl bg-[#D9381E]/6 flex items-center justify-center mx-auto">
          <Ticket className="w-7 h-7 text-[#D9381E]" />
        </div>
        <p className="font-[family-name:var(--font-display)] text-7xl text-[#D9381E] tracking-tight">
          404
        </p>
        <div className="border-t border-dashed border-[#E8E3DC] w-16 mx-auto" />
        <div className="space-y-1">
          <p className="font-[family-name:var(--font-display)] text-xl text-[#1A1817]">
            Page Not Found
          </p>
          <p className="text-[#8B8580] text-sm">
            The page you&apos;re looking for doesn&apos;t exist or has been moved.
          </p>
        </div>
        <Link href="/" className="btn-accent inline-flex items-center gap-2 px-6 py-2.5">
          Back to Events
        </Link>
      </div>
    </div>
  );
}

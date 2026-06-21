"use client";

import { useParams } from "next/navigation";
import Link from "next/link";

export default function ConfirmationPage() {
  const { id } = useParams<{ id: string }>();
  return (
    <div className="max-w-md mx-auto mt-20 text-center">
      <h1 className="text-3xl font-bold mb-4">Booking Confirmed!</h1>
      <p className="mb-2">Booking ID: <span className="font-mono text-sm">{id}</span></p>
      <Link href="/" className="underline">Back to Events</Link>
    </div>
  );
}

import type { Metadata } from "next";
import { DM_Serif_Display, Nunito } from "next/font/google";
import "./globals.css";
import { AuthProvider } from "@/lib/auth-context";
import { Header } from "@/components/layout/header";
import { ToastProvider } from "@/components/ui/toast";
import { QueryProvider } from "@/lib/query-provider";

const serif = DM_Serif_Display({
  subsets: ["latin"],
  weight: "400",
  variable: "--font-display",
});

const sans = Nunito({
  subsets: ["latin"],
  weight: ["400", "500", "600", "700", "800"],
  variable: "--font-body",
});

export const metadata: Metadata = {
  title: "TicketBus",
  description: "Event ticketing platform",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={`${serif.variable} ${sans.variable} h-full antialiased`}>
      <body className="min-h-full flex flex-col bg-white text-[#16224A]">
        <QueryProvider>
          <ToastProvider>
            <AuthProvider>
              <Header />
              <main className="flex-1 container py-10">{children}</main>
            </AuthProvider>
          </ToastProvider>
        </QueryProvider>
      </body>
    </html>
  );
}

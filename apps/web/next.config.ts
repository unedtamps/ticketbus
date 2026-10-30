import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  images: { unoptimized: true },
  experimental: {
    optimizePackageImports: ["lucide-react"],
  },
  allowedDevOrigins: [
    "archived-characteristics-leg-completing.trycloudflare.com",
  ],
};

export default nextConfig;

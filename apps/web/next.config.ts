import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
  images: { unoptimized: true },
  experimental: {
    optimizePackageImports: ["lucide-react"],
  },
  allowedDevOrigins: [
    "archived-characteristics-leg-completing.trycloudflare.com",
    "127.0.0.1",
  ],
};

export default nextConfig;

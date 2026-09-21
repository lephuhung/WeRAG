import type { NextConfig } from "next";

const backend = process.env.WERAG_BACKEND_URL ?? "http://10.10.0.241:18080";

const nextConfig: NextConfig = {
  reactStrictMode: true,
  allowedDevOrigins: ["127.0.0.1"],
  async rewrites() {
    // Dev + single-origin deploy parity with Vue (vite proxy + nginx):
    // /api/* and /files/* go to the Go backend.
    // Go app serves on APP_PORT (default 8080, this env uses 18080);
    // port 8080 here is nginx forcing https redirects.
    return [
      { source: "/files/:path*", destination: `${backend}/files/:path*` },
    ];
  },
};

export default nextConfig;

import type { NextConfig } from "next";

const backend = process.env.WERAG_BACKEND_URL ?? "http://localhost:18080";

const nextConfig: NextConfig = {
  reactStrictMode: true,
  outputFileTracingRoot: __dirname,
  async rewrites() {
    // Dev + single-origin deploy parity with Vue (vite proxy + nginx):
    // /api/* and /files/* go to the Go backend.
    // Go app serves on APP_PORT (default 8080, this env uses 18080);
    // port 8080 here is nginx forcing https redirects.
    return [
      { source: "/api/:path*", destination: `${backend}/api/:path*` },
      { source: "/files/:path*", destination: `${backend}/files/:path*` },
    ];
  },
};

export default nextConfig;

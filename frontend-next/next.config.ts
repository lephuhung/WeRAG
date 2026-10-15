import type { NextConfig } from "next";

// Go backend the /api and /files rewrites point at. Dev falls back to the
// backend's default APP_PORT on this machine; a production build/start must
// name it explicitly (rewrites are baked into the build), so fail loudly
// rather than ship a build that proxies to the wrong host.
const backendEnv = process.env.WERAG_BACKEND_URL?.trim();
if (!backendEnv && process.env.NODE_ENV === "production") {
  throw new Error(
    "WERAG_BACKEND_URL is not set: set it to the Go backend URL (e.g. http://127.0.0.1:8080) " +
      "for `next build` / `next start`. See frontend-next/.env.example.",
  );
}
const backend = backendEnv || "http://127.0.0.1:8080";

// Hosts allowed to load dev assets (next dev); comma-separated.
const devOrigins = (process.env.WERAG_DEV_ORIGINS ?? "")
  .split(",")
  .map((s) => s.trim())
  .filter(Boolean);

const nextConfig: NextConfig = {
  reactStrictMode: true,
  // Next's built-in compression wraps every response — including rewritten
  // /api SSE streams — in gzip and buffers it whole, so chat "streams" only
  // render once the turn ends. Disable it; production compresses at nginx.
  compress: false,
  allowedDevOrigins: devOrigins.length > 0 ? devOrigins : ["127.0.0.1", "localhost"],
  async rewrites() {
    // Dev + single-origin deploy parity with Vue (vite proxy + nginx):
    // /api/* and /files/* go to the Go backend.
    // Go app serves on APP_PORT (default 8080).
    return [
      { source: "/api/:path*", destination: `${backend}/api/:path*` },
      { source: "/files/:path*", destination: `${backend}/files/:path*` },
    ];
  },
};

export default nextConfig;

import { type NextRequest } from "next/server";

export const dynamic = "force-dynamic";

const BACKEND = process.env.WERAG_BACKEND_URL ?? "http://10.10.0.241:18080";

async function proxy(req: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  const { path } = await params;
  const search = req.nextUrl.search;
  const targetUrl = `${BACKEND}/api/${path.join("/")}${search}`;

  const headers = new Headers();
  req.headers.forEach((value, key) => {
    // Strip host header so upstream uses its own host/port
    if (key.toLowerCase() !== "host") {
      headers.set(key, value);
    }
  });

  const init: RequestInit = {
    method: req.method,
    headers,
    redirect: "manual",
  };

  if (req.method !== "GET" && req.method !== "HEAD") {
    init.body = req.body;
    // @ts-expect-error duplex is required in Node fetch when body is a stream
    init.duplex = "half";
  }

  const upstreamRes = await fetch(targetUrl, init);

  const resHeaders = new Headers();
  upstreamRes.headers.forEach((value, key) => {
    resHeaders.set(key, value);
  });

  // Disable buffering for SSE streams
  const contentType = resHeaders.get("content-type") || "";
  if (contentType.includes("text/event-stream")) {
    resHeaders.set("Cache-Control", "no-cache, no-transform");
    resHeaders.set("Connection", "keep-alive");
    resHeaders.set("X-Accel-Buffering", "no");
  }

  return new Response(upstreamRes.body, {
    status: upstreamRes.status,
    statusText: upstreamRes.statusText,
    headers: resHeaders,
  });
}

export const GET = proxy;
export const POST = proxy;
export const PUT = proxy;
export const DELETE = proxy;
export const PATCH = proxy;
export const HEAD = proxy;
export const OPTIONS = proxy;

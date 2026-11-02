import { renderWordManifest, resolveWordAddinOrigin } from "@/lib/word-addin/manifest";

export const dynamic = "force-dynamic";

export function GET(request: Request) {
  try {
    const origin = resolveWordAddinOrigin(
      process.env.WERAG_WORD_ADDIN_ORIGIN,
      new URL(request.url).origin,
      process.env.NODE_ENV === "development",
    );
    return new Response(renderWordManifest(origin), {
      headers: {
        "Content-Type": "application/xml; charset=utf-8",
        "Content-Disposition": 'attachment; filename="werag-word-addin.xml"',
        "Cache-Control": "no-store",
        "X-Content-Type-Options": "nosniff",
      },
    });
  } catch {
    return new Response("Chưa cấu hình địa chỉ HTTPS cho add-in Word. Vui lòng liên hệ quản trị viên.", {
      status: 503,
      headers: { "Content-Type": "text/plain; charset=utf-8", "Cache-Control": "no-store" },
    });
  }
}

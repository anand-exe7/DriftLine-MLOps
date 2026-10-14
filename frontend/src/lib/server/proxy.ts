// Server-side reverse proxy used by the /go and /ml route handlers.
//
// Why not next.config rewrites: rewrites are resolved at build time and can't
// add headers, but the Go API may require an API key that must never reach the
// browser. Here the key is read from the server's environment at request time.

const HOP_BY_HOP = ["connection", "keep-alive", "transfer-encoding", "upgrade", "host", "content-length"];

export async function proxy(req: Request, base: string, path: string[], apiKey?: string): Promise<Response> {
  const url = new URL(req.url);
  const target = `${base.replace(/\/$/, "")}/${path.map(encodeURIComponent).join("/")}${url.search}`;

  const headers = new Headers(req.headers);
  for (const h of HOP_BY_HOP) headers.delete(h);
  headers.delete("cookie"); // backends don't use the dashboard's cookies
  if (apiKey) headers.set("X-API-Key", apiKey);

  const hasBody = !["GET", "HEAD"].includes(req.method);
  let upstream: Response;
  try {
    upstream = await fetch(target, {
      method: req.method,
      headers,
      body: hasBody ? req.body : undefined,
      // stream uploads (model.onnx) straight through instead of buffering them
      ...(hasBody ? { duplex: "half" } : {}),
      redirect: "manual",
      cache: "no-store",
    } as RequestInit);
  } catch (err) {
    return Response.json({ error: `upstream unreachable: ${(err as Error).message}` }, { status: 502 });
  }

  const out = new Headers(upstream.headers);
  // fetch already decoded the body, so these would now be wrong
  out.delete("content-encoding");
  out.delete("content-length");
  return new Response(upstream.body, { status: upstream.status, statusText: upstream.statusText, headers: out });
}

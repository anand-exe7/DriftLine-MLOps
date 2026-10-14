import { proxy } from "@/lib/server/proxy";

// /go/* -> Go control plane (registry, deployments, predictions, drift, alerts)
const GO_URL = process.env.GO_API_URL ?? "http://localhost:8080";

async function handle(req: Request, { params }: { params: Promise<{ path: string[] }> }) {
  const { path } = await params;
  return proxy(req, GO_URL, path, process.env.DRIFTLINE_API_KEY);
}

export { handle as GET, handle as POST, handle as PATCH, handle as PUT, handle as DELETE };

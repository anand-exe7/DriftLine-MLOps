import { proxy } from "@/lib/server/proxy";

// /ml/* -> Python ml-service side port (health, models, metrics, predict)
const ML_URL = process.env.ML_SERVICE_URL ?? "http://localhost:8000";

async function handle(req: Request, { params }: { params: Promise<{ path: string[] }> }) {
  const { path } = await params;
  return proxy(req, ML_URL, path);
}

export { handle as GET, handle as POST };

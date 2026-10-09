import type { NextConfig } from "next";

// The browser only ever talks to this Next.js origin. These rewrites proxy to
// the backends, so there is no CORS setup and no backend URL baked into the JS.
//   /go/*  -> Go control plane   (registry, alerts, healthz)
//   /ml/*  -> Python ml-service  (health, models, metrics, predict)
const GO_URL = process.env.GO_API_URL ?? "http://localhost:8080";
const ML_URL = process.env.ML_SERVICE_URL ?? "http://localhost:8000";

const nextConfig: NextConfig = {
  // Every page here is a live client-side dashboard polling the backends, so
  // there is nothing worth prerendering or caching; cacheComponents would
  // only force Suspense boundaries around usePathname/useParams.
  output: "standalone",
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
  async rewrites() {
    return [
      { source: "/go/:path*", destination: `${GO_URL}/:path*` },
      { source: "/ml/:path*", destination: `${ML_URL}/:path*` },
    ];
  },
};

export default nextConfig;

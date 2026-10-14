import type { NextConfig } from "next";

// The browser only ever talks to this Next.js origin. /go/* and /ml/* are
// proxied by route handlers (src/app/go, src/app/ml) that read GO_API_URL,
// ML_SERVICE_URL and DRIFTLINE_API_KEY at runtime, so one image works in any
// environment and the API key stays on the server.

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
};

export default nextConfig;

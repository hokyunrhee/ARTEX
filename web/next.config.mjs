import { fileURLToPath } from "node:url";

// Static export: `NEXT_EXPORT=1 next build` writes static files to web/out for deployment
// to an nginx web root. Leave this unset for next dev to retain the /api proxy and hot reload.
const isExport = process.env.NEXT_EXPORT === "1";
// Vercel demo: all pages use mock data, with no backend or /api proxy.
const isMock = process.env.NEXT_PUBLIC_MOCK === "1";

/** @type {import('next').NextConfig} */
const nextConfig = {
  // Prevent a parent lockfile from affecting root detection and generated asset paths.
  turbopack: { root: fileURLToPath(new URL(".", import.meta.url)) },
  reactCompiler: true,
  // Allow LAN IP addresses to access development assets and HMR; adjust as needed.
  // Allow any IPv4 origin for /_next/* and HMR during development, including changing LAN addresses.
  // Next rejects a bare "*" for security; use segment wildcards: "*.*.*.*" matches any IPv4 address.
  allowedDevOrigins: ["*.*.*.*"],
  compiler: {
    removeConsole: process.env.NODE_ENV === "production",
  },
};

if (isExport) {
  // Static export produces route/index.html files without runtime image optimization.
  nextConfig.output = "export";
  nextConfig.images = { unoptimized: true };
  nextConfig.trailingSlash = true;
} else if (isMock) {
  // The mock demo has no backend or API proxy.
  nextConfig.images = { unoptimized: true };
} else {
  // Development proxies API calls to the configured Go backend.
  nextConfig.rewrites = async () => {
    const backend = process.env.AUTOPENTEST_API ?? "http://localhost:8787";
    return [{ source: "/api/:path*", destination: `${backend}/api/:path*` }];
  };
}

export default nextConfig;

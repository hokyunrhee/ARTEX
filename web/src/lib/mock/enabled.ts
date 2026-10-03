// Mock switch, injected at build time. The NEXT_PUBLIC_ prefix exposes it to the browser.
// Set NEXT_PUBLIC_MOCK=1 on Vercel to use mock data throughout, without a backend.
export const MOCK = process.env.NEXT_PUBLIC_MOCK === "1";

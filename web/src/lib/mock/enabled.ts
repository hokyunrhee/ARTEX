// Mock switch. A public variable injected at build time (only the NEXT_PUBLIC_ prefix is readable in the browser).
// Set NEXT_PUBLIC_MOCK=1 on Vercel to run the whole site on mock data with no backend.
export const MOCK = process.env.NEXT_PUBLIC_MOCK === "1";

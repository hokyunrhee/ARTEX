export class ApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

// Plain errors from legacy adapters retain a narrow English-text fallback.
export function isApiError(error: unknown, status: number, fallback: string) {
  if (!(error instanceof Error)) return false;
  if ("status" in error) return error.status === status;
  return error.message.toLowerCase().includes(fallback.toLowerCase());
}

// Legacy reasons survive in archives and immutable approval audit snapshots.
export function isModelReason(reason?: string) {
  return /^\[(?:model|模型)\]/.test(reason ?? "");
}

export function stripModelReason(reason?: string) {
  return (reason ?? "").replace(/^\[(?:model|模型)\]\s*/, "");
}

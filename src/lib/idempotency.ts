type BrowserCrypto = Pick<Crypto, "getRandomValues"> &
  Partial<Pick<Crypto, "randomUUID">>;

let fallbackSequence = 0;

/**
 * Creates an opaque request identifier without requiring a secure browser
 * context. randomUUID is unavailable on some HTTP deployments, while
 * getRandomValues remains available in most browsers.
 */
export function createIdempotencyKey(
  cryptoAPI: BrowserCrypto | null | undefined = globalThis.crypto,
): string {
  if (typeof cryptoAPI?.randomUUID === "function")
    return cryptoAPI.randomUUID();

  if (typeof cryptoAPI?.getRandomValues === "function") {
    const bytes = new Uint8Array(16);
    cryptoAPI.getRandomValues(bytes);
    bytes[6] = (bytes[6] & 0x0f) | 0x40;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;
    const hex = Array.from(bytes, (byte) =>
      byte.toString(16).padStart(2, "0"),
    ).join("");
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
  }

  fallbackSequence = (fallbackSequence + 1) >>> 0;
  return `reset-${Date.now().toString(36)}-${fallbackSequence.toString(36)}-${Math.random().toString(36).slice(2, 14)}`;
}

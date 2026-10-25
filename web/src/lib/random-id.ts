// Browser identifiers must also work on HTTP LAN pages. getRandomValues keeps
// the full 128 bits of randomness without requiring a secure-context API.
export function randomID() {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
    byte.toString(16).padStart(2, '0'),
  ).join('')
}

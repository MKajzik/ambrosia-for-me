import "server-only";

/** The API rejects larger bodies itself; refusing here spares buffering them. */
export const MAX_BODY_BYTES = 64 * 1024;

/**
 * Reads a request body, giving up (and cancelling the rest of it) as soon as it
 * passes `limit` bytes, so an oversized or endless chunked body is never buffered.
 * Returns null when the body is too large.
 */
export async function readBounded(req: Request, limit: number): Promise<ArrayBuffer | null> {
  if (!req.body) return new ArrayBuffer(0);
  const reader = req.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > limit) {
      await reader.cancel();
      return null;
    }
    chunks.push(value);
  }
  const out = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return out.buffer;
}

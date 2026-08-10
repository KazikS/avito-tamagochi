// UUID v4 для идемпотентности (actionId, Idempotency-Key).
//
// Не `crypto.randomUUID()` напрямую: он существует только в secure context —
// HTTPS или localhost. Демо-стенд отдаётся по HTTP с голого IP (домена и
// сертификата нет), и там `crypto.randomUUID` === undefined, из-за чего любое
// действие ухода падало с «crypto.randomUUID is not a function».
//
// `crypto.getRandomValues`, в отличие от него, доступен и без secure context,
// поэтому запасной путь остаётся криптографически стойким — это не Math.random().
export function newUuid(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }

  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);

  // Версия (4) и вариант (RFC 4122) — те же биты, что проставил бы randomUUID.
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;

  const hex: string[] = [];
  for (const byte of bytes) {
    hex.push(byte.toString(16).padStart(2, '0'));
  }

  return [
    hex.slice(0, 4).join(''),
    hex.slice(4, 6).join(''),
    hex.slice(6, 8).join(''),
    hex.slice(8, 10).join(''),
    hex.slice(10, 16).join(''),
  ].join('-');
}

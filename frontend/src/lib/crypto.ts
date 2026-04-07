// AES-256-GCM encryption/decryption matching the Go backend.
// Uses Web Crypto API for browser-native performance.

const ENCRYPTION_SECRET = process.env.NEXT_PUBLIC_ENCRYPTION_SECRET || "lotus-exchange-2026-aes-secret-key";

let cachedKey: CryptoKey | null = null;

async function getKey(): Promise<CryptoKey> {
  if (cachedKey) return cachedKey;

  const encoder = new TextEncoder();
  const keyData = encoder.encode(ENCRYPTION_SECRET);
  const hash = await crypto.subtle.digest("SHA-256", keyData);

  cachedKey = await crypto.subtle.importKey(
    "raw",
    hash,
    { name: "AES-GCM" },
    false,
    ["encrypt", "decrypt"]
  );

  return cachedKey;
}

// Safe base64 encode that handles large Uint8Arrays without stack overflow
function uint8ToBase64(bytes: Uint8Array): string {
  let binary = "";
  const chunkSize = 8192;
  for (let i = 0; i < bytes.length; i += chunkSize) {
    const chunk = bytes.subarray(i, Math.min(i + chunkSize, bytes.length));
    for (let j = 0; j < chunk.length; j++) {
      binary += String.fromCharCode(chunk[j]);
    }
  }
  return btoa(binary);
}

function base64ToUint8(b64: string): Uint8Array {
  const binary = atob(b64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

export async function encryptData(data: unknown): Promise<string> {
  try {
    const key = await getKey();
    const encoder = new TextEncoder();
    const plaintext = encoder.encode(JSON.stringify(data));

    const iv = crypto.getRandomValues(new Uint8Array(12));

    const ciphertext = await crypto.subtle.encrypt(
      { name: "AES-GCM", iv },
      key,
      plaintext
    );

    // Prepend IV to ciphertext (same format as Go backend)
    const combined = new Uint8Array(iv.length + new Uint8Array(ciphertext).length);
    combined.set(iv);
    combined.set(new Uint8Array(ciphertext), iv.length);

    return uint8ToBase64(combined);
  } catch (err) {
    throw err;
  }
}

export async function decryptData<T>(encrypted: string): Promise<T> {
  const key = await getKey();
  const combined = base64ToUint8(encrypted);

  const iv = combined.slice(0, 12);
  const ciphertext = combined.slice(12);

  const plaintext = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv },
    key,
    ciphertext
  );

  const decoder = new TextDecoder();
  return JSON.parse(decoder.decode(plaintext));
}

// Encrypt data for localStorage (sync XOR obfuscation)
export function encryptLocalStorage(key: string, value: string): void {
  if (typeof window === "undefined") return;
  try {
    const encoded = uint8ToBase64(
      new Uint8Array(
        Array.from(value).map((c, i) =>
          c.charCodeAt(0) ^ ENCRYPTION_SECRET.charCodeAt(i % ENCRYPTION_SECRET.length)
        )
      )
    );
    localStorage.setItem(key, encoded);
  } catch {
    // Fallback to plain storage
    localStorage.setItem(key, value);
  }
}

export function decryptLocalStorage(key: string): string | null {
  if (typeof window === "undefined") return null;
  const stored = localStorage.getItem(key);
  if (!stored) return null;
  try {
    const bytes = base64ToUint8(stored);
    return Array.from(bytes)
      .map((b, i) =>
        String.fromCharCode(b ^ ENCRYPTION_SECRET.charCodeAt(i % ENCRYPTION_SECRET.length))
      )
      .join("");
  } catch {
    // Fallback: return raw value (migration from unencrypted)
    return stored;
  }
}

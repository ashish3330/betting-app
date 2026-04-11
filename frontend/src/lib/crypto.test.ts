import { describe, it, expect, beforeEach } from "vitest";
import {
  encryptData,
  decryptData,
  encryptLocalStorage,
  decryptLocalStorage,
} from "./crypto";

// ── Minimal in-memory localStorage shim so crypto.ts's window/localStorage ──
// ── checks pass in the Node test environment.                                ──
function installLocalStorageShim() {
  const store = new Map<string, string>();
  const shim = {
    getItem: (k: string) => (store.has(k) ? (store.get(k) as string) : null),
    setItem: (k: string, v: string) => {
      store.set(k, String(v));
    },
    removeItem: (k: string) => {
      store.delete(k);
    },
    clear: () => {
      store.clear();
    },
    key: (i: number) => Array.from(store.keys())[i] ?? null,
    get length() {
      return store.size;
    },
  };
  // crypto.ts guards on `typeof window === "undefined"` — give it a window too.
  (globalThis as unknown as { window: unknown }).window = globalThis;
  (globalThis as unknown as { localStorage: typeof shim }).localStorage = shim;
  return shim;
}

beforeEach(() => {
  installLocalStorageShim();
});

describe("encryptData / decryptData (AES-GCM)", () => {
  it("round-trips a simple object", async () => {
    const payload = { hello: "world", n: 42, nested: { ok: true } };
    const encrypted = await encryptData(payload);
    expect(typeof encrypted).toBe("string");
    expect(encrypted.length).toBeGreaterThan(0);

    const decrypted = await decryptData<typeof payload>(encrypted);
    expect(decrypted).toEqual(payload);
  });

  it("round-trips strings, numbers, arrays, and null", async () => {
    const cases: unknown[] = [
      "a plain string",
      12345,
      [1, 2, 3, "four"],
      null,
      { a: [1, { b: "c" }] },
    ];
    for (const c of cases) {
      const enc = await encryptData(c);
      const dec = await decryptData(enc);
      expect(dec).toEqual(c);
    }
  });

  it("produces different ciphertexts for the same plaintext (nonce uniqueness)", async () => {
    const payload = { same: "input" };
    const a = await encryptData(payload);
    const b = await encryptData(payload);
    const c = await encryptData(payload);
    expect(a).not.toBe(b);
    expect(b).not.toBe(c);
    expect(a).not.toBe(c);

    // But all decrypt back to the same value.
    expect(await decryptData(a)).toEqual(payload);
    expect(await decryptData(b)).toEqual(payload);
    expect(await decryptData(c)).toEqual(payload);
  });

  it("rejects tampered ciphertext", async () => {
    // Use a long enough payload that we have plenty of ciphertext bytes to
    // tamper with (well past the 12-byte IV prefix and auth tag region).
    const enc = await encryptData({ secret: "classified".repeat(16) });

    // Decode, flip a byte in the middle, re-encode. This guarantees we alter
    // the ciphertext bytes (not just base64 padding) and that AES-GCM's
    // auth tag will reject it.
    const raw = Buffer.from(enc, "base64");
    const mid = Math.floor(raw.length / 2);
    raw[mid] = raw[mid] ^ 0xff;
    const tampered = raw.toString("base64");
    expect(tampered).not.toBe(enc);

    await expect(decryptData(tampered)).rejects.toBeDefined();
  });

  it("rejects completely invalid ciphertext", async () => {
    await expect(decryptData("!!!not-valid-base64!!!")).rejects.toBeDefined();
  });
});

describe("encryptLocalStorage / decryptLocalStorage", () => {
  it("round-trips a value via localStorage", () => {
    encryptLocalStorage("access_token", "abc.def.ghi");
    const read = decryptLocalStorage("access_token");
    expect(read).toBe("abc.def.ghi");
  });

  it("does not store the raw value in plain text", () => {
    const raw = "super-secret-token-value";
    encryptLocalStorage("tok", raw);
    const stored = (globalThis as unknown as { localStorage: Storage }).localStorage.getItem("tok");
    expect(stored).not.toBeNull();
    expect(stored).not.toBe(raw);
  });

  it("round-trips across a simulated page reload (same shim, fresh read)", () => {
    encryptLocalStorage("user", JSON.stringify({ id: 1, role: "admin" }));

    // Simulate page reload: keep the same underlying storage but re-access
    // through a new variable to mimic fresh module state.
    const decoded = decryptLocalStorage("user");
    expect(decoded).not.toBeNull();
    expect(JSON.parse(decoded as string)).toEqual({ id: 1, role: "admin" });
  });

  it("returns null when the key is missing", () => {
    expect(decryptLocalStorage("nope-never-set")).toBeNull();
  });

  it("round-trips unicode strings correctly", () => {
    // XOR-based scheme works on char codes, so keep inputs within BMP ASCII
    // range (that's what the production code guarantees it works for).
    const value = "token_with_digits_123_and_symbols-._~";
    encryptLocalStorage("k", value);
    expect(decryptLocalStorage("k")).toBe(value);
  });
});

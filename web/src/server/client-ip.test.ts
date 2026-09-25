import { describe, expect, it } from "vitest";
import { forwardedClientIp } from "./client-ip";

describe("forwardedClientIp", () => {
  it("ignores the header when no proxy is trusted", () => {
    expect(forwardedClientIp("9.9.9.9", 0)).toBeNull();
  });

  it("takes the Nth entry from the right", () => {
    expect(forwardedClientIp("1.1.1.1, 2.2.2.2", 1)).toBe("2.2.2.2");
    expect(forwardedClientIp("1.1.1.1, 2.2.2.2, 3.3.3.3", 2)).toBe("2.2.2.2");
  });

  it("never uses entries a client could have forged", () => {
    expect(forwardedClientIp("6.6.6.6, 7.7.7.7", 1)).toBe("7.7.7.7");
  });

  it("returns null for a missing, short or malformed header", () => {
    expect(forwardedClientIp(null, 1)).toBeNull();
    expect(forwardedClientIp("1.1.1.1", 2)).toBeNull();
    expect(forwardedClientIp("not-an-ip", 1)).toBeNull();
    expect(forwardedClientIp("", 1)).toBeNull();
  });

  it("accepts IPv6", () => {
    expect(forwardedClientIp("2001:db8::1", 1)).toBe("2001:db8::1");
  });
});

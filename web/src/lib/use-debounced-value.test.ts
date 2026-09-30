// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDebouncedValue } from "./use-debounced-value";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("useDebouncedValue", () => {
  it("starts with the value and follows it only after it has settled", () => {
    const { result, rerender } = renderHook(({ v }) => useDebouncedValue(v, 250), { initialProps: { v: "a" } });
    expect(result.current).toBe("a");

    rerender({ v: "ab" });
    act(() => void vi.advanceTimersByTime(200));
    expect(result.current).toBe("a");

    rerender({ v: "abc" });
    act(() => void vi.advanceTimersByTime(200));
    expect(result.current).toBe("a");

    act(() => void vi.advanceTimersByTime(60));
    expect(result.current).toBe("abc");
  });
});

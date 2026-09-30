// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useToday } from "./use-today";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("useToday", () => {
  it("is the local calendar day even late in the evening, when the UTC day may already be another", () => {
    vi.setSystemTime(new Date(2026, 8, 30, 23, 30));
    const { result } = renderHook(() => useToday());
    expect(result.current).toBe("2026-09-30");
  });

  it("moves to the next day once midnight passes while the page stays open", () => {
    vi.setSystemTime(new Date(2026, 8, 30, 23, 59, 30));
    const { result } = renderHook(() => useToday());
    expect(result.current).toBe("2026-09-30");
    act(() => void vi.advanceTimersByTime(60_000));
    expect(result.current).toBe("2026-10-01");
  });

  it("keeps the same value while the day has not changed", () => {
    vi.setSystemTime(new Date(2026, 8, 30, 10, 0));
    const { result } = renderHook(() => useToday());
    act(() => void vi.advanceTimersByTime(5 * 60_000));
    expect(result.current).toBe("2026-09-30");
  });
});

// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAutosave } from "./use-autosave";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

type Props = { value: string | null; dirty: boolean };
const tick = (ms: number) =>
  act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });

function setup(save: (value: string) => Promise<void>) {
  return renderHook((props: Props) => useAutosave({ ...props, save, delayMs: 700 }), { initialProps: { value: "a", dirty: false } as Props });
}

describe("useAutosave", () => {
  it("waits for the edit to settle, then saves the latest value once", async () => {
    const save = vi.fn(async () => {});
    const { rerender } = setup(save);

    rerender({ value: "ab", dirty: true });
    await tick(500);
    rerender({ value: "abc", dirty: true });
    await tick(600);
    expect(save).not.toHaveBeenCalled();

    await tick(200);
    expect(save).toHaveBeenCalledTimes(1);
    expect(save).toHaveBeenCalledWith("abc");

    rerender({ value: "abc", dirty: false });
    await tick(5000);
    expect(save).toHaveBeenCalledTimes(1);
  });

  it("never saves the same value twice, even if the caller still calls it dirty", async () => {
    const save = vi.fn(async () => {});
    const { rerender } = setup(save);
    rerender({ value: "b", dirty: true });
    await tick(700);
    await tick(5000);
    expect(save).toHaveBeenCalledTimes(1);
  });

  it("does not save when nothing is dirty or the draft is not valid (null)", async () => {
    const save = vi.fn(async () => {});
    const { rerender } = setup(save);
    rerender({ value: "b", dirty: false });
    await tick(5000);
    rerender({ value: null, dirty: true });
    await tick(5000);
    expect(save).not.toHaveBeenCalled();
  });

  it("saves an edit made during a save only after that save finishes", async () => {
    let release!: () => void;
    const save = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          release = resolve;
        }),
    );
    const { result, rerender } = setup(save);

    rerender({ value: "b", dirty: true });
    await tick(700);
    expect(save).toHaveBeenCalledTimes(1);
    expect(result.current.saving).toBe(true);

    rerender({ value: "c", dirty: true });
    await tick(3000);
    expect(save).toHaveBeenCalledTimes(1);

    await act(async () => release());
    await tick(700);
    expect(save).toHaveBeenCalledTimes(2);
    expect(save).toHaveBeenLastCalledWith("c");
  });

  it("reports a failure, does not hammer the server with the same value, retries on the next edit and on demand", async () => {
    const save = vi.fn().mockRejectedValueOnce(new Error("offline")).mockRejectedValueOnce(new Error("still offline")).mockResolvedValue(undefined);
    const { result, rerender } = setup(save);

    rerender({ value: "b", dirty: true });
    await tick(700);
    expect(save).toHaveBeenCalledTimes(1);
    expect(result.current.error).toBeInstanceOf(Error);
    expect(result.current.saving).toBe(false);

    await tick(10_000);
    expect(save).toHaveBeenCalledTimes(1);

    rerender({ value: "c", dirty: true });
    await tick(700);
    expect(save).toHaveBeenCalledTimes(2);
    expect(result.current.error).toBeInstanceOf(Error);

    await act(async () => {
      await result.current.retry();
    });
    expect(save).toHaveBeenCalledTimes(3);
    expect(save).toHaveBeenLastCalledWith("c");
    expect(result.current.error).toBeNull();
  });

  it("saves right away on flush, without waiting for the pause", async () => {
    const save = vi.fn(async () => {});
    const { result, rerender } = setup(save);
    rerender({ value: "b", dirty: true });
    await act(async () => {
      await result.current.flush();
    });
    expect(save).toHaveBeenCalledWith("b");
  });

  it("saves a pending edit when the editor is closed before the pause is over", () => {
    const save = vi.fn(async () => {});
    const { rerender, unmount } = setup(save);
    rerender({ value: "b", dirty: true });
    expect(save).not.toHaveBeenCalled();
    unmount();
    expect(save).toHaveBeenCalledWith("b");
  });
});

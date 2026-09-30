import { vi } from "vitest";

type Listener = (event: Event) => void;

/** A stand-in for the browser's `EventSource` that tests drive by hand. Install it with `FakeEventSource.install()`. */
export class FakeEventSource {
  static instances: FakeEventSource[] = [];
  readonly url: string;
  /** 0 connecting, 1 open, 2 closed, like the real one. */
  readyState = 0;
  private listeners = new Map<string, Set<Listener>>();

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: Listener) {
    const set = this.listeners.get(type) ?? new Set<Listener>();
    set.add(listener);
    this.listeners.set(type, set);
  }

  removeEventListener(type: string, listener: Listener) {
    this.listeners.get(type)?.delete(listener);
  }

  close() {
    this.readyState = 2;
  }

  /** A named event with a JSON body, the way the API sends them. */
  message(type: string, data: unknown) {
    this.raw(type, JSON.stringify(data));
  }

  raw(type: string, text: string) {
    this.dispatch(new MessageEvent(type, { data: text }));
  }

  open() {
    this.readyState = 1;
    this.dispatch(new Event("open"));
  }

  /** The connection dropped. `permanent` is what a browser does after an HTTP error answer: it gives up (closed) instead of reconnecting. */
  fail(permanent: boolean) {
    this.readyState = permanent ? 2 : 0;
    this.dispatch(new Event("error"));
  }

  private dispatch(event: Event) {
    for (const listener of this.listeners.get(event.type) ?? []) listener(event);
  }

  static install() {
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
    return FakeEventSource;
  }

  static get last(): FakeEventSource | undefined {
    return FakeEventSource.instances.at(-1);
  }
}

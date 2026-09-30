import { describe, expect, it } from "vitest";
import { EVENT_TYPES, parseListEvent } from "./list-events";

describe("parseListEvent", () => {
  it("reads the four event types the API sends", () => {
    expect(EVENT_TYPES).toEqual(["item_changed", "item_deleted", "list_changed", "list_deleted"]);
    expect(parseListEvent('{"type":"item_changed","list_id":"l1","item_id":"i1","version":4}')).toEqual({ type: "item_changed", list_id: "l1", item_id: "i1", version: 4 });
    expect(parseListEvent('{"type":"item_deleted","list_id":"l1","item_id":"i1","version":2}')).toEqual({ type: "item_deleted", list_id: "l1", item_id: "i1", version: 2 });
    expect(parseListEvent('{"type":"list_changed","list_id":"l1"}')).toEqual({ type: "list_changed", list_id: "l1" });
    expect(parseListEvent('{"type":"list_deleted","list_id":"l1"}')).toEqual({ type: "list_deleted", list_id: "l1" });
  });

  it.each(["", "not json", "null", "42", '"text"', "[]", '{"type":"item_changed"}', '{"type":"nonsense","list_id":"l1"}', '{"list_id":"l1"}', '{"type":7,"list_id":"l1"}'])("refuses %j", (text) => {
    expect(parseListEvent(text)).toBeNull();
  });

  it("drops members of the wrong type instead of trusting them", () => {
    expect(parseListEvent('{"type":"item_changed","list_id":"l1","item_id":5,"version":"4"}')).toEqual({ type: "item_changed", list_id: "l1" });
  });
});

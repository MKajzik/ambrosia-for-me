import { describe, expect, it } from "vitest";
import { addDays, formatLongDate, formatMonthDay, formatWeekRange, formatWeekday, isIsoDate, parseIsoDate, startOfWeek, toIsoDate, today, weekDates } from "./dates";

describe("toIsoDate and today", () => {
  it("names the local calendar day at any hour, never the UTC one", () => {
    expect(toIsoDate(new Date(2026, 9, 5, 0, 0, 0))).toBe("2026-10-05");
    expect(toIsoDate(new Date(2026, 9, 5, 12, 0, 0))).toBe("2026-10-05");
    expect(toIsoDate(new Date(2026, 9, 5, 23, 59, 59))).toBe("2026-10-05");
    expect(today(new Date(2026, 0, 1, 23, 30))).toBe("2026-01-01");
  });

  it("pads month and day", () => {
    expect(toIsoDate(new Date(2026, 2, 3))).toBe("2026-03-03");
  });
});

describe("parseIsoDate and isIsoDate", () => {
  it("round-trips a real date as local midnight", () => {
    const date = parseIsoDate("2026-09-30");
    expect([date.getFullYear(), date.getMonth(), date.getDate(), date.getHours()]).toEqual([2026, 8, 30, 0]);
    expect(toIsoDate(date)).toBe("2026-09-30");
  });

  it.each(["2026-02-30", "2026-13-01", "2026-00-10", "2026-9-30", "30-09-2026", "tomorrow", "", "2026-09-30T10:00:00Z", "0099-01-01"])("refuses %j", (text) => {
    expect(isIsoDate(text)).toBe(false);
    expect(() => parseIsoDate(text)).toThrow();
  });

  it("knows leap days", () => {
    expect(isIsoDate("2028-02-29")).toBe(true);
    expect(isIsoDate("2026-02-29")).toBe(false);
  });
});

describe("addDays", () => {
  it("crosses month, year and leap-day boundaries", () => {
    expect(addDays("2026-12-31", 1)).toBe("2027-01-01");
    expect(addDays("2028-02-28", 1)).toBe("2028-02-29");
    expect(addDays("2028-03-01", -1)).toBe("2028-02-29");
    expect(addDays("2026-09-30", 0)).toBe("2026-09-30");
  });

  it.each(["2026-03-01", "2026-03-25", "2026-10-20", "2026-11-01"])("gives sixty distinct, consecutive days from %s, across any daylight-saving change", (start) => {
    let previous = start;
    const seen = new Set([start]);
    for (let i = 1; i <= 60; i++) {
      const next = addDays(start, i);
      expect(next).toBe(addDays(previous, 1));
      expect(parseIsoDate(next).getTime()).toBeGreaterThan(parseIsoDate(previous).getTime());
      seen.add(next);
      previous = next;
    }
    expect(seen.size).toBe(61);
  });
});

describe("weeks start on Monday", () => {
  it("finds the Monday on or before a date", () => {
    expect(startOfWeek("2026-10-05")).toBe("2026-10-05");
    expect(startOfWeek("2026-10-04")).toBe("2026-09-28");
    expect(startOfWeek("2026-09-30")).toBe("2026-09-28");
    expect(startOfWeek("2026-01-01")).toBe("2025-12-29");
  });

  it("lists seven consecutive dates from the start", () => {
    expect(weekDates("2026-09-28")).toEqual(["2026-09-28", "2026-09-29", "2026-09-30", "2026-10-01", "2026-10-02", "2026-10-03", "2026-10-04"]);
    for (const start of ["2026-03-23", "2026-10-26"]) {
      const week = weekDates(start);
      expect(week).toHaveLength(7);
      expect(parseIsoDate(week[0] ?? "").getDay()).toBe(1);
      expect(parseIsoDate(week[6] ?? "").getDay()).toBe(0);
    }
  });
});

describe("formatting", () => {
  it("writes dates the way a person would read them", () => {
    expect(formatWeekday("2026-09-30")).toBe("Wed");
    expect(formatMonthDay("2026-09-30")).toBe("Sep 30");
    expect(formatLongDate("2026-09-30")).toBe("Wednesday, September 30");
    expect(formatWeekRange("2026-09-28")).toBe("Sep 28 – Oct 4");
  });
});

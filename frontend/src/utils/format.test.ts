import { describe, expect, it } from "vitest";

import { fmtDate, fmtDateTime, isOnline, relativeTime } from "./format";

describe("fmtDate", () => {
  it("formats as DD.MM.YYYY", () => {
    // Midday local time avoids any timezone date-shift.
    expect(fmtDate("2026-12-31T12:00:00")).toBe("31.12.2026");
    expect(fmtDate("2026-01-05T12:00:00")).toBe("05.01.2026");
  });

  it("returns em dash for null/undefined", () => {
    expect(fmtDate(null)).toBe("—");
    expect(fmtDate(undefined)).toBe("—");
  });
});

describe("fmtDateTime", () => {
  it("formats as DD.MM.YYYY HH:MM in 24h", () => {
    expect(fmtDateTime("2026-01-05T09:07:00")).toBe("05.01.2026 09:07");
    expect(fmtDateTime("2026-01-05T23:45:00")).toBe("05.01.2026 23:45");
  });

  it("returns em dash for null", () => {
    expect(fmtDateTime(null)).toBe("—");
  });
});

describe("isOnline", () => {
  it("is true for a very recent timestamp", () => {
    expect(isOnline(new Date().toISOString())).toBe(true);
  });

  it("is false for an old timestamp", () => {
    const old = new Date(Date.now() - 60 * 60 * 1000).toISOString();
    expect(isOnline(old)).toBe(false);
  });

  it("is false for null", () => {
    expect(isOnline(null)).toBe(false);
  });
});

describe("relativeTime", () => {
  it("says 'never' for null", () => {
    expect(relativeTime(null)).toBe("never");
  });

  it("says 'just now' for the present", () => {
    expect(relativeTime(new Date().toISOString())).toBe("just now");
  });

  it("reports minutes and hours", () => {
    expect(relativeTime(new Date(Date.now() - 5 * 60 * 1000).toISOString())).toBe("5m ago");
    expect(relativeTime(new Date(Date.now() - 3 * 60 * 60 * 1000).toISOString())).toBe("3h ago");
  });
});

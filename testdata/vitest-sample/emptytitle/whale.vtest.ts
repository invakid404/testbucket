import { test, expect } from "vitest";
// A whale carrying a LEGAL empty-title test — Vitest accepts test("", ...) and
// `vitest list --json` reports it as {"name":""}. It must be scheduled and run
// exactly once when the file is name-sliced, never silently dropped (P1).
test("", () => { expect(1).toBe(1); });
test("named one", () => { expect(1).toBe(1); });
test("named two", () => { expect(1).toBe(1); });

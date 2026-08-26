import { test, expect } from "vitest";
// Top-level await on a promise that never resolves: importing this module (what
// `vitest list` collection does) blocks forever. Glob discovery does not import
// it, so `vitest list --filesOnly` returns it without hanging.
await new Promise<void>(() => {});
test("never collected", () => { expect(1).toBe(1); });

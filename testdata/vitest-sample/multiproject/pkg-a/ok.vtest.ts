import { describe, test, expect } from "vitest";
// pkg-a is the HEALTHY project of the multi-project deadlock fixture: its module
// imports fine, so it is runnable and name-sliceable. It carries a few tests
// (incl. a nested describe) so a slice-gate test can split it across buckets and
// prove Runnables reaches its names WITHOUT importing the sibling pkg-b, whose
// module never finishes importing. Glob discovery still sees exactly one file
// here, so the discovery deadlock test is unaffected.
test("ok", () => { expect(1).toBe(1); });
test("alpha", () => { expect(1).toBe(1); });
describe("group", () => {
  test("beta", () => { expect(1).toBe(1); });
});

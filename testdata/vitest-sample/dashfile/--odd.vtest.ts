import { test, expect } from "vitest";
// The filename starts with "--" ON PURPOSE: its root-relative id is `--odd.vtest.ts`,
// which a bare `vitest list --odd.vtest.ts` reads as an unknown CLI option. This is
// the regression fixture for the `./`-prefixed positional filter in Runnables.
test("odd one", () => { expect(1).toBe(1); });
test("odd two", () => { expect(2).toBe(2); });

import { defineConfig } from "vitest/config";

// A multi-project config: the exact shape that makes `vitest list` (full
// collection) deadlock on some setups. `vitest list --filesOnly` (glob) resolves
// each project's include without importing a line of test code, so it is immune.
export default defineConfig({
  test: {
    projects: [
      { test: { name: "a", include: ["pkg-a/**/*.vtest.ts"] } },
      { test: { name: "b", include: ["pkg-b/**/*.vtest.ts"] } },
    ],
  },
});

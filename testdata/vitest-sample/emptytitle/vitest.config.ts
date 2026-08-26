import { defineConfig } from "vitest/config";
// A single-project config that includes this dir's `.vtest.ts` specs. The
// `.vtest.ts` suffix keeps these fixtures OUT of the flat sample's default
// `.spec.ts`/`.test.ts` discovery, so they only appear when this dir is the root.
export default defineConfig({
  test: { include: ["**/*.vtest.ts"] },
});

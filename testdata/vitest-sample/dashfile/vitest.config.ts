import { defineConfig } from "vitest/config";
// A single-project config whose one spec's root-relative id starts with "-"
// (`--odd.vtest.ts` sits at this dir's root). Passed as a bare `vitest list`
// positional, that id is misread by CAC as an option ("Unknown option --odd");
// the adapter `./`-prefixes the filter so it scopes correctly. The `.vtest.ts`
// suffix keeps it OUT of the flat sample's default `.spec.ts` discovery, so it
// only appears when this dir is the root.
export default defineConfig({
  test: { include: ["**/*.vtest.ts"] },
});

import { defineConfig } from "vitest/config";

// The Mandel shape, reduced: one `projects` entry whose include glob spans the
// whole tree. The specs use the sample's `.vtest.ts` convention so the parent
// sample root's DEFAULT discovery (`**/*.{test,spec}.ts`) cannot pick them up;
// only the directory layout — the suffix-sharing part — has to match Mandel, and
// the four exact Mandel `.test.ts` paths are pinned offline in
// exact_paths_test.go. That matters — with a `projects` config Vitest forwards only a
// fixed whitelist of CLI options into each project, and `include`, `exclude` and
// `dir` are NOT on it. The file universe is therefore fixed by this file, and a
// positional filter is the ONLY selector an invocation has.
export default defineConfig({
  test: {
    projects: [
      {
        test: {
          name: "unit",
          include: ["**/*.vtest.ts"],
          exclude: ["**/node_modules/**", "**/dist/**"],
        },
      },
    ],
  },
});

import { defineConfig } from "vitest/config";

// A project whose ROOT is a child of the workspace root. Vitest filters each
// project's files with `relative(project.config.dir || project.config.root, …)`,
// NOT with paths relative to the workspace root, so an adapter that compares
// workspace-relative ids sees different strings than Vitest does.
export default defineConfig({
  test: {
    projects: [
      {
        root: "./projects/unit",
        test: { name: "unit", include: ["**/*.vtest.ts"] },
      },
    ],
  },
});

package planbind

import ()

// discoveryJSON is what `vitest list --filesOnly --json` prints: the raw bytes
// a bundle freezes.
const discoveryJSON = `[{"file":"tests/alpha.spec.ts"},{"file":"tests/beta.spec.ts"},{"file":"tests/gamma.spec.ts"}]`

// storeJSON is a warm store: three measured targets, one of them flagged for
// name slicing so the runnable-listing input actually matters.
const storeJSON = `{
  "schema": 1,
  "flags": "vitest",
  "updated_at": "2026-08-30T00:00:00Z",
  "units": {
    "tests/alpha.spec.ts": {"seconds": 90, "samples": 4, "split": "run", "split_into": 2,
      "tests": {"alpha one": 60, "alpha two": 30}},
    "tests/beta.spec.ts": {"seconds": 40, "samples": 4},
    "tests/gamma.spec.ts": {"seconds": 20, "samples": 4}
  }
}`

// testCommit is a FULL commit SHA: the bundle refuses an abbreviation, because
// a prefix is something another object can grow into.
const testCommit = "d9ae1d433bb45012c04d567879b66fc4bf6112c6"

// runnableJSON is what `vitest list <file> --json` prints for the sliced file.
const runnableJSON = `[{"name":"alpha one","file":"tests/alpha.spec.ts"},{"name":"alpha two","file":"tests/alpha.spec.ts"}]`

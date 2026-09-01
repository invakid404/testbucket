# Frozen Mandel lock excerpt

`pnpm-lock.yaml` here is a byte-exact EXCERPT of the frozen profile's real
lockfile: `mandel-ai/mandel` at `d9ae1d433bb45012c04d567879b66fc4bf6112c6`,
blob `cbb87097e3b93c2572a6df8e3283427938d9d951`, 1,281,715 bytes, whose
`packages:` entries are reproduced here unmodified.

It exists because the small hand-written fixtures elsewhere could not establish
that the production parser reads the lockfile the contract actually names. The
full lock is 1.25 MB and mostly irrelevant to this parser; what matters is its
SHAPE, and the nodes kept here are exactly the ones that carry it:

- **`@ai-sdk/provider-utils` at both `2.2.8` and `3.0.30`.** This is the pair
  that broke the name-keyed closure: it could not represent two versions of
  one package and refused the whole lock, so no real Stage-1 source profile
  for this target could ever be derived.
- **A `snapshots:` section with multi-peer nodes.** pnpm v9 keeps resolution
  metadata in `packages:` and the actual dependency graph in `snapshots:`,
  where a package appears once per peer context it was resolved under. A
  parser that read only the first section looked complete and silently dropped
  every peer node — 474 of them in the full lock, including two of `vitest`
  itself and two of `@vitest/mocker`. Both of those pairs are kept here.
- **`xlsx@https://cdn.sheetjs.com/…`, the lock's one node with no integrity.**
  It is also the one node whose key is a URL rather than a version, so it
  carries an explicit `version: 0.20.3`. It exercises two rules at once: a
  version must be read from the entry's own field rather than split out of its
  key, and a node the lock does not pin is refused unless the receipt declares
  that exception explicitly.
- **The complete Vitest 4.1.10 family** — `vitest` plus every `@vitest/*` the
  façade loads — so the frozen-version rule is exercised against the real
  entries rather than against invented ones.
- **Ordinary nodes** (`chai`, `expect-type`, `pathe`, `std-env`,
  `tinyrainbow`, `zod`) so the excerpt is a normal lock rather than a shape
  built only from the interesting cases.

Entry bodies are copied verbatim in both sections, including `resolution:`
integrity/tarball mappings, `engines:`, `peerDependencies:`,
`peerDependenciesMeta:` and snapshot `dependencies:`, so the parser meets the
same line shapes it meets in the real file.

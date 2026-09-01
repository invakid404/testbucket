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
  that broke the previous name-keyed closure: it could not represent two
  versions of one package and refused the whole lock, so no real Stage-1
  source profile for this target could ever be derived.
- **The complete Vitest 4.1.10 family** — `vitest` plus every `@vitest/*` the
  façade loads — so the frozen-version rule is exercised against the real
  entries rather than against invented ones.
- **Six ordinary nodes** (`chai`, `expect-type`, `pathe`, `std-env`,
  `tinyrainbow`, `zod`) so the excerpt is a normal lock rather than a shape
  built only from the interesting cases.

Entry bodies are copied verbatim, including `resolution:` integrity/tarball
mappings, `engines:`, `peerDependencies:` and `peerDependenciesMeta:`, so the
parser meets the same line shapes it meets in the real file.

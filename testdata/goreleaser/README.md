# Representative goreleaser artifact manifest

`artifacts.json` is the shape `goreleaser release` writes to `dist/` for this
project's `.goreleaser.yaml`: four platform **Binary** rows, the four
**Archive** rows built from them, and one **Checksum** row.

It is committed because the defect it guards against is a cross-layer one, and
neither layer alone could show it. The campaign gate selected files by artifact
TYPE while the publisher selected them by GLOB, so the two sets differed by
exactly the four raw `Binary` rows — gated, never uploaded, and therefore able
to satisfy the campaign's delivered-binary match with a file no consumer ever
receives. A test needs a manifest containing all three types to demonstrate
that the two selectors now agree.

The paths use goreleaser's real `dist/<project>_<goos>_<goarch>_<version>/`
binary layout so the derivation meets the same shapes it meets in a release.

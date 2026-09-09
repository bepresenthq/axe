# BePresent's axe fork

This is the maintained preview adapter for [runbp](https://github.com/bepresenthq/runbp), based on [k-kohey/axe](https://github.com/k-kohey/axe). It is unrelated to cameroncooke/AXe.

The `runbp` branch starts at upstream v0.0.14, commit `53bdd66c3522660b35df8f932d84d5a30c875a60`. The first fork commit imports runbp's existing adapter patch without changing its behavior. Upstream history and the Go module name remain intact to make future merges easier. The upstream license remains in LICENSE.

## Build

On an Apple Silicon Mac with Xcode, Node 26+, Go and protobuf installed:

```sh
node scripts/build-runbp.mjs
.output/axe --version
```

The build fetches the pinned idb protocol, verifies its SHA-256, generates Go bindings, runs the runbp unit tests, builds both Swift analysis helpers in one SwiftPM build, and compiles `v0.0.14-runbp.2`. It does not launch a simulator. Pass an absolute output path as the first argument to build for runbp's private tool cache. Native use also needs `idb-companion`. Keep `axe-parser` and `axe-index-reader` beside the built `axe` executable when installing or packaging it. Runbp releases use these bundled helpers instead of downloading or compiling them during the first preview open.

## Maintenance

- Keep preview host, compiler integration and control-protocol changes here. Runbp owns orchestration, browser input, streaming, source revisions and evidence.
- Fetch upstream explicitly and review changes before merging or cherry-picking onto `runbp`. Do not automatically track upstream's default branch.
- Use tags such as `runbp-v0.0.14.1` for fork releases. This avoids upstream's `v*` release workflow, which also publishes its VS Code extension.
- Bump the version in the build script for a release. Run the build and the applicable native acceptance checks in runbp, then create an immutable release tag. Never move a released tag.
- Update runbp's `scripts/preview-backend/source.json` with the tag, full commit and binary version together. Runbp verifies the checkout commit and keeps each revision in a separate tool directory.

The initial release preserves previously tested renderer code. Native Xcode comparison testing was stopped at the user's request; this fork extraction does not claim a new native acceptance run.

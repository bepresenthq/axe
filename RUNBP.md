# BePresent's axe fork

This is the maintained preview adapter for [runbp](https://github.com/bepresenthq/runbp), based on [k-kohey/axe](https://github.com/k-kohey/axe). It is unrelated to cameroncooke/AXe.

The `runbp` branch starts at upstream v0.0.14, commit `53bdd66c3522660b35df8f932d84d5a30c875a60`. The first fork commit imports runbp's existing adapter patch without changing its behavior. Upstream history and the Go module name remain intact to make future merges easier. The upstream license remains in LICENSE.

## Build

On an Apple Silicon Mac with Xcode, Node 26+, Go and protobuf installed:

```sh
node scripts/build-runbp.mjs
.output/axe --version
```

The build fetches the pinned idb protocol, verifies its SHA-256, generates Go bindings, runs the runbp unit tests, builds both Swift analysis helpers in one SwiftPM build, and compiles `v0.0.15-runbp.3`. It does not launch a simulator. Pass an absolute output path as the first argument to build for runbp's private tool cache. Native use also needs `idb-companion`. Keep `axe-parser` and `axe-index-reader` beside the built `axe` executable when installing or packaging it. Runbp releases use these bundled helpers instead of downloading or compiling them during the first preview open.

## Maintenance

- Keep preview host, compiler integration and control-protocol changes here. Runbp owns orchestration, browser input, streaming, source revisions and evidence.
- Fetch upstream explicitly and review changes before merging or cherry-picking onto `runbp`. Do not automatically track upstream's default branch.
- Use tags such as `runbp-v0.0.14.1` for fork releases. This avoids upstream's `v*` release workflow, which also publishes its VS Code extension.
- Bump the version in the build script for a release. Run the build and the applicable native acceptance checks in runbp, then create an immutable release tag. Never move a released tag.
- Update runbp's `scripts/preview-backend/source.json` with the tag, full commit and binary version together. Runbp verifies the checkout commit and keeps each revision in a separate tool directory.

The initial release preserves previously tested renderer code. Native Xcode comparison testing was stopped at the user's request; this fork extraction does not claim a new native acceptance run.

## Simulator host capabilities

The replacement host embeds Xcode's resolved simulator entitlements from the staged executable in its own Mach-O `__TEXT,__entitlements` section before ad-hoc signing. App-group names and other capability values are preserved. A declared entitlement file without embedded simulator metadata produces an actionable error instead of a host missing its declared capabilities.

The host keeps built resources and Info.plist keys except for its launch screen, main storyboard and scene configuration. Production App/delegate startup remains bypassed. The `axe.` bundle identity and rewritten extension IDs can affect keychain, URL routing, cloud and notification services. Capability warnings describe the required full-app verification; they do not claim entitlement metadata makes a service available.

Run runbp's `npm run test:preview:native` with `RUNBP_AXE` pointing to this build to exercise shared-container writes after reset, app crashes, backend restart and host regeneration.

### Resolved host identity

Before each controlled host launch, axe atomically writes `host.json` beside `request.json` and `result.json` in `RUNBP_CONTROL_DIR`. The object has `schema: "runbp.preview.host.v1"`, `bundleId`, `originalBundleId`, `targetName`, `deviceId`, `backendPid`, and `launchRequestedAt` in UTC RFC3339Nano. Optional `requestId` and `revision` identify the request that initiated launch. Consumers must match the owning backend PID and device; request identity is diagnostic because hot reloads can reuse the same host. The file records an attempted launch, not proof that the app is alive. It remains available if launch or the native layout barrier fails.

Build settings are selected from one application target block. Extension/framework settings cannot fill missing app values. Schemes with multiple application targets are rejected with guidance to choose a single-app scheme.

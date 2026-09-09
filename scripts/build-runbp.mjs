#!/usr/bin/env node
// Build the runbp adapter with pinned protocol inputs. No simulator is launched.
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
const scratch = path.resolve(import.meta.dirname, "..");
const binary = path.resolve(
  process.argv[2] || path.join(scratch, ".output/axe"),
);
const destination = path.dirname(binary);
const toolDirectory = fs.mkdtempSync(
  path.join(os.tmpdir(), "runbp-axe-tools-"),
);
const run = (command, args, cwd, env = process.env) => {
  const result = spawnSync(command, args, { cwd, env, stdio: "inherit" });
  if (result.status !== 0) throw new Error(`${command} failed`);
};
try {
  const tools = path.join(toolDirectory, "bin");
  fs.mkdirSync(tools);
  const env = {
    ...process.env,
    GOBIN: tools,
    PATH: tools + path.delimiter + process.env.PATH,
  };
  run(
    "go",
    ["install", "google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11"],
    scratch,
    env,
  );
  run(
    "go",
    ["install", "google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1"],
    scratch,
    env,
  );
  const response = await fetch(
    "https://raw.githubusercontent.com/facebook/idb/e10a90af5372dbcd404eb2fbc38fa3c88630fbd7/proto/idb.proto",
  );
  if (!response.ok) throw new Error("could not download pinned idb protocol");
  const proto = await response.text();
  if (
    createHash("sha256").update(proto).digest("hex") !==
    "77f6fb7f56ba3d0dda095b1565e2781de30def8e6c47538e1f4fd3be688d986c"
  )
    throw new Error("idb protocol checksum mismatch");
  fs.mkdirSync(path.join(scratch, "cmd/internal/idb/proto"), {
    recursive: true,
  });
  fs.writeFileSync(
    path.join(scratch, "cmd/internal/idb/proto/idb.proto"),
    proto.replace(
      "package idb;",
      'package idb;\noption go_package = "github.com/k-kohey/axe/internal/idb/idbproto";',
    ),
  );
  run(
    "protoc",
    [
      "--go_out=cmd",
      "--go_opt=module=github.com/k-kohey/axe",
      "--go-grpc_out=cmd",
      "--go-grpc_opt=module=github.com/k-kohey/axe",
      "--proto_path=cmd/internal/idb/proto",
      "cmd/internal/idb/proto/idb.proto",
    ],
    scratch,
    env,
  );
  run(
    "go",
    [
      "test",
      "./internal/preview/build",
      "./internal/preview/analysis",
      "./internal/preview",
      "-run",
      "TestRunbp|TestPreviewDiscoveryAndBuildUseMatchingAppleSiliconSettings",
    ],
    path.join(scratch, "cmd"),
  );
  // Build both helpers together so SwiftPM shares dependency compilation.
  // Bundle them beside axe; preview open must not bootstrap SwiftSyntax.
  const analysisPackage = path.join(
    scratch,
    "cmd/internal/preview/analysis/swift-analysis",
  );
  const analysisBuild = path.join(toolDirectory, "swift-analysis");
  run(
    "swift",
    [
      "build",
      "-c",
      "release",
      "--package-path",
      analysisPackage,
      "--scratch-path",
      analysisBuild,
    ],
    scratch,
  );
  fs.mkdirSync(destination, { recursive: true });
  for (const product of ["axe-parser", "axe-index-reader"]) {
    const staged = path.join(destination, product + ".tmp");
    fs.copyFileSync(path.join(analysisBuild, "release", product), staged);
    fs.chmodSync(staged, 0o755);
    fs.renameSync(staged, path.join(destination, product));
  }
  run(
    "go",
    [
      "build",
      "-ldflags",
      "-X main.version=v0.0.14-runbp.2",
      "-o",
      binary + ".tmp",
      "./axe",
    ],
    path.join(scratch, "cmd"),
  );
  fs.renameSync(binary + ".tmp", binary);
} finally {
  fs.rmSync(toolDirectory, { recursive: true, force: true });
}
console.log(binary);

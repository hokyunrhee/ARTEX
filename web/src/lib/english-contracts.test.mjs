import { ApiError, isApiError } from "./api-error.ts";
import { isModelReason, stripModelReason } from "./approval-source.ts";
import assert from "node:assert/strict";
import test from "node:test";

test("model reasons classify and display current and legacy row and audit snapshots", () => {
  for (const prefix of ["[model]", "[模型]"]) {
    assert.equal(isModelReason(`${prefix} Reason`), true);
    assert.equal(stripModelReason(`${prefix} Reason`), "Reason");
  }
  assert.equal(isModelReason("Rule reason"), false);
  assert.equal(isModelReason(undefined), false);
  assert.equal(stripModelReason(undefined), "");
  assert.equal(stripModelReason("Rule reason"), "Rule reason");
});

test("HTTP status controls skill overwrite and restored-archive pruning", () => {
  assert.equal(isApiError(new ApiError("Custom message", 409), 409, "already exists"), true);
  assert.equal(isApiError(new ApiError("Custom message", 404), 404, "Archive not found"), true);
  assert.equal(isApiError(new ApiError("Already exists", 500), 409, "already exists"), false);
  assert.equal(isApiError(new ApiError("Archive not found", 401), 404, "Archive not found"), false);
  assert.equal(isApiError(new Error("Skill already exists: sample"), 409, "already exists"), true);
  assert.equal(isApiError(new Error("Archive not found"), 404, "Archive not found"), true);
  assert.equal(isApiError(new Error("Network failed"), 404, "Archive not found"), false);
  assert.equal(isApiError(null, 404, "Archive not found"), false);
});

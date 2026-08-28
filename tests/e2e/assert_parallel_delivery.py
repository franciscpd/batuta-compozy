#!/usr/bin/env python3
"""Validate evidence emitted by the coherent Go parallel-delivery harness.

This module is deliberately not a lifecycle simulator. The Go integration
test owns routing, journal, Git worktree, integration, and cleanup mutations;
this consumer only rejects malformed evidence it emits.
"""

import argparse
import json
import re


TASKS = {"task_01", "task_02", "task_03", "task_04", "task_05"}
FIRST_WAVE = {"task_01", "task_02", "task_03", "task_04"}
CONVENTIONAL_COMMIT = re.compile(r"^(?:feat|fix|test|docs)(?:\([a-z0-9-]+\))?!?: .+")
SHA256 = re.compile(r"^sha256:[0-9a-f]{64}$")
GIT_SHA = re.compile(r"^[0-9a-f]{40}$")
SEMVER = re.compile(r"^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$")


def _mapping(value: object, label: str) -> dict:
    assert isinstance(value, dict), f"{label} must be an object"
    return value


def validate_harness_evidence(value: object) -> None:
    evidence = _mapping(value, "harness evidence")
    if set(evidence) == {"width_probe"}:
        validate_width_probe_evidence(evidence["width_probe"])
        return
    assert set(evidence) == {
        "identity", "initial_tasks", "initial_worktrees", "frontend_route", "continuation",
        "child_starts", "conflict", "integrated", "commits", "dependent", "cleanup", "replay",
    }, f"unexpected harness evidence keys: {sorted(evidence)}"
    identity = _mapping(evidence["identity"], "identity")
    assert identity.get("scenario_id") == "parallel-demo", "scenario identity is not canonical"
    assert isinstance(identity.get("delivery_id"), str) and SHA256.match(identity["delivery_id"]), "delivery identity is not content-addressed"
    assert isinstance(identity.get("extension_version"), str) and SEMVER.match(identity["extension_version"]), "extension descriptor version is malformed"
    assert isinstance(identity.get("question_operation_id"), str) and SHA256.match(identity["question_operation_id"]), "typed question operation identity is missing"
    assert isinstance(identity.get("reviewed_head"), str) and GIT_SHA.match(identity["reviewed_head"]), "reviewed publication head is invalid"
    assert identity.get("retained_worktree_id") == "wt_parallel_03", "retained diagnostic worktree identity is unstable"
    assert isinstance(identity.get("retained_worktree_root"), str) and identity["retained_worktree_root"], "retained diagnostic worktree root is missing"
    assert set(evidence["initial_tasks"]) == FIRST_WAVE and evidence["initial_worktrees"] == 4, "initial wave is not capped at four"
    starts = _mapping(evidence["child_starts"], "child starts")
    assert starts.get("count") == 4 and set(starts.get("ids", [])) == {
        "run_started_task_01_1", "run_started_task_02_1", "run_started_task_03_1", "run_started_task_04_1",
    }, "child starts are not four stable initial identities"
    route = _mapping(evidence["frontend_route"], "frontend route")
    assert route.get("provider") == "cursor" and isinstance(route.get("model"), str) and route["model"].startswith("grok-"), "frontend route is not Cursor/Grok"
    continuation = _mapping(evidence["continuation"], "continuation")
    assert continuation == {"typed": True, "same_child": True, "same_worktree": True, "sibling_progress": True, "physical_execution": 2}, "typed continuation evidence is incomplete"
    conflict = _mapping(evidence["conflict"], "conflict")
    assert conflict.get("task_id") == "task_02" and conflict.get("accepted_task_ids") == ["task_01"] and conflict.get("retry_execution") == 3, "prefix conflict/retry evidence is invalid"
    assert set(evidence["integrated"]) == TASKS, "not all five tasks integrated"
    commits = _mapping(evidence["commits"], "commits")
    assert set(commits) == TASKS and all(isinstance(subject, str) and CONVENTIONAL_COMMIT.match(subject) for subject in commits.values()), "implementation commits are not exactly one Conventional Commit per task"
    dependent = _mapping(evidence["dependent"], "dependent")
    assert dependent == {"task_id": "task_05", "admitted_after_prerequisites": True}, "dependent admission evidence is invalid"
    cleanup = _mapping(evidence["cleanup"], "cleanup")
    assert cleanup.get("retained") is True and cleanup.get("blocker_code") == "worktree_evidence_changed", "retention evidence is invalid"
    assert evidence["replay"] == {"cleanup_journal_unchanged": True, "cleanup_removes_unchanged": True}, "replay mutated cleanup evidence"


def validate_width_probe_evidence(value: object) -> None:
    probe = _mapping(value, "width probe")
    assert set(probe) == {
        "eligible_task_ids", "started_child_ids", "started_child_count",
        "pending_task_id", "pending_task_attempts", "prepare_replay_stable", "create_calls",
    }, f"unexpected width probe keys: {sorted(probe)}"
    assert set(probe["eligible_task_ids"]) == TASKS and len(probe["eligible_task_ids"]) == 5, "width probe lacks five independent eligible tasks"
    assert probe["started_child_count"] == 4 and set(probe["started_child_ids"]) == {
        "run_started_task_01_1", "run_started_task_02_1", "run_started_task_03_1", "run_started_task_04_1",
    }, "width probe did not preserve exactly four child starts"
    assert probe["pending_task_id"] == "task_05" and probe["pending_task_attempts"] == 0, "fifth eligible task was not left pending"
    assert probe["prepare_replay_stable"] is True and probe["create_calls"] == 4, "width prepare/starter replay changed mutations"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("evidence")
    args = parser.parse_args()
    with open(args.evidence, encoding="utf-8") as handle:
        validate_harness_evidence(json.load(handle))
    print("OK: coherent Go harness evidence")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (AssertionError, OSError, json.JSONDecodeError) as error:
        raise SystemExit(f"parallel delivery violation: {error}") from error

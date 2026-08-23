#!/usr/bin/env python3
"""Validate the publication gate from public loop-run events."""
# CLI: assert_publication_gate.py --events <loop-run-events.json> \
#          --decision approve|reject
# From the run's SSE event export (a JSON array), assert in order:
# 1. a needs_approval event for node publish_gate exists;
# 2. no node_running event for node publish precedes it;
# 3. with --decision approve: a gate_verdict event for publish_gate with
#    verdict approve, then node_succeeded for publish, the final
#    status_changed carries done, and the publish node_succeeded carries
#    publication evidence (see NOTE below);
# 4. with --decision reject: a gate_verdict with verdict reject and the
#    final status_changed carries blocked, with NO node_running for publish;
# 5. exit non-zero with a one-line reason on the first violated assert.
#
# NOTE on publication evidence: the daemon's public loop-run event stream
# does NOT carry node output for a successfully completed goal node. Checked
# against the compozy daemon source: LoopNodeTerminalPayload.Details (the
# only free-form field on a node_succeeded/node_failed event) is populated
# by internal/daemon/loop_hook_observer.go only for the blocked/exhausted
# Goal dispositions (dispatchSettledGoalNodeTerminal); the normal
# OnTaskRunTerminal path used for a *completed* Goal node never sets
# Details, so head_sha/op_ids/pr_url/compare_url are not present on today's
# default success payload. This validator therefore checks the one place the
# schema allows evidence to travel — the node_succeeded event's own
# `content.details` object — and fails closed: an event stream that (like
# today's default daemon payload) carries no `details` on that event is
# treated as evidence-free and REJECTED, exactly as an events file that
# omits the evidence deliberately would be. Passing this check requires an
# events export that was enriched with publication evidence in `details`
# (e.g. by an operator-side capture step that merges compozy__loop_status's
# node output into the corresponding node_succeeded event) — this is a
# forward-looking contract check, not a claim that today's unmodified daemon
# export already satisfies it.

import argparse
from dataclasses import dataclass
import json
import sys


GATE_NODE_ID = "publish_gate"
PUBLISH_NODE_ID = "publish"


@dataclass(frozen=True)
class ValidationResult:
    gate_park_sequence: int
    verdict_sequence: int
    final_status_sequence: int
    final_status: str


def event_type(event: dict) -> str | None:
    value = event.get("type")
    return value if isinstance(value, str) else None


def content(event: dict) -> dict:
    value = event.get("content", {})
    return value if isinstance(value, dict) else {}


def sequence(event: dict) -> int:
    value = event.get("sequence")
    assert isinstance(value, int), f"event has invalid sequence: {value!r}"
    return value


def node_id(event: dict) -> str | None:
    value = content(event).get("node_id")
    return value if isinstance(value, str) else None


def verdict(event: dict) -> str | None:
    value = content(event).get("verdict")
    return value if isinstance(value, str) else None


def status(event: dict) -> str | None:
    value = content(event).get("status")
    return value if isinstance(value, str) else None


def ordered_events(events: list[dict]) -> list[dict]:
    assert isinstance(events, list), "loop-run events must be a JSON list"
    return sorted(events, key=sequence)


def events_of_type(events: list[dict], expected_type: str) -> list[dict]:
    return [event for event in events if event_type(event) == expected_type]


def gate_park(events: list[dict]) -> dict:
    parks = [
        event
        for event in events_of_type(events, "needs_approval")
        if node_id(event) == GATE_NODE_ID
    ]
    assert parks, f"missing needs_approval event for node {GATE_NODE_ID!r}"
    return parks[0]


def assert_no_publish_run_before(events: list[dict], before_sequence: int) -> None:
    for event in events_of_type(events, "node_running"):
        if node_id(event) != PUBLISH_NODE_ID:
            continue
        assert sequence(event) > before_sequence, (
            f"node_running for node {PUBLISH_NODE_ID!r} at sequence {sequence(event)} "
            f"precedes needs_approval for {GATE_NODE_ID!r} at sequence {before_sequence}"
        )


def find_gate_verdict(events: list[dict], expected_verdict: str) -> dict:
    verdicts = [
        event
        for event in events_of_type(events, "gate_verdict")
        if node_id(event) == GATE_NODE_ID and verdict(event) == expected_verdict
    ]
    assert verdicts, f"missing gate_verdict for {GATE_NODE_ID!r} with verdict {expected_verdict!r}"
    return verdicts[0]


def final_status_changed(events: list[dict]) -> dict:
    changes = events_of_type(events, "status_changed")
    assert changes, "missing status_changed event"
    return changes[-1]


def publish_evidence(publish_success_event: dict) -> dict:
    value = content(publish_success_event).get("details")
    return value if isinstance(value, dict) else {}


def assert_publish_evidence(publish_success_event: dict) -> None:
    """Assert node_succeeded for publish carries non-empty push evidence.

    See the module NOTE above: `details` is the only free-form field the
    daemon's node_succeeded event carries, and today's default success
    payload leaves it empty. A missing/empty `details`, a missing
    `head_sha`, or a missing `pr_url`/`compare_url` are all treated as
    evidence-free and rejected — an evidence-free "success" must never pass.
    """
    evidence = publish_evidence(publish_success_event)
    head_sha = evidence.get("head_sha")
    assert isinstance(head_sha, str) and head_sha, (
        f"missing head_sha in node_succeeded 'details' for node {PUBLISH_NODE_ID!r}: "
        f"{evidence!r}"
    )
    pr_url = evidence.get("pr_url")
    compare_url = evidence.get("compare_url")
    has_pr_url = isinstance(pr_url, str) and bool(pr_url)
    has_compare_url = isinstance(compare_url, str) and bool(compare_url)
    assert has_pr_url or has_compare_url, (
        "missing publication URL (pr_url or compare_url) in node_succeeded "
        f"'details' for node {PUBLISH_NODE_ID!r}: {evidence!r}"
    )


def validate_gate(events: list[dict], decision: str) -> ValidationResult:
    assert decision in ("approve", "reject"), f"unknown decision: {decision!r}"
    events = ordered_events(events)

    park = gate_park(events)
    park_sequence = sequence(park)
    assert_no_publish_run_before(events, park_sequence)

    if decision == "approve":
        verdict_event = find_gate_verdict(events, "approve")
        verdict_sequence = sequence(verdict_event)

        successes = [
            event
            for event in events_of_type(events, "node_succeeded")
            if node_id(event) == PUBLISH_NODE_ID and sequence(event) > verdict_sequence
        ]
        assert successes, (
            f"missing node_succeeded for node {PUBLISH_NODE_ID!r} after gate_verdict "
            f"at sequence {verdict_sequence}"
        )

        final = final_status_changed(events)
        final_sequence = sequence(final)
        final_value = status(final)
        assert final_value == "done", (
            f"final status_changed at sequence {final_sequence} carries {final_value!r}, "
            "expected done"
        )

        assert_publish_evidence(successes[-1])
    else:
        verdict_event = find_gate_verdict(events, "reject")
        verdict_sequence = sequence(verdict_event)

        final = final_status_changed(events)
        final_sequence = sequence(final)
        final_value = status(final)
        assert final_value == "blocked", (
            f"final status_changed at sequence {final_sequence} carries {final_value!r}, "
            "expected blocked"
        )

        runs = [
            event for event in events_of_type(events, "node_running") if node_id(event) == PUBLISH_NODE_ID
        ]
        assert not runs, (
            f"node_running for node {PUBLISH_NODE_ID!r} at sequence {sequence(runs[0])} "
            "occurred after a reject; publish must never run"
        )

    return ValidationResult(
        gate_park_sequence=park_sequence,
        verdict_sequence=verdict_sequence,
        final_status_sequence=final_sequence,
        final_status=final_value,
    )


def load_events(path: str) -> list[dict]:
    with open(path, encoding="utf-8") as handle:
        return json.load(handle)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--events", required=True)
    parser.add_argument("--decision", required=True, choices=["approve", "reject"])
    args = parser.parse_args()

    events = load_events(args.events)
    result = validate_gate(events, args.decision)
    print(
        "OK: publication gate "
        f"decision={args.decision} "
        f"gate_park_sequence={result.gate_park_sequence} "
        f"verdict_sequence={result.verdict_sequence} "
        f"final_status_sequence={result.final_status_sequence} "
        f"final_status={result.final_status}"
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (AssertionError, OSError, json.JSONDecodeError) as error:
        print(f"publication gate violation: {error}", file=sys.stderr)
        raise SystemExit(1) from error

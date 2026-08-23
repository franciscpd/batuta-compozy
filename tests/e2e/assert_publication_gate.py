#!/usr/bin/env python3
"""Validate the publication gate from public loop-run events."""
# CLI: assert_publication_gate.py --events <loop-run-events.json> \
#          --decision approve|reject
# From the run's SSE event export (a JSON array), assert in order:
# 1. a needs_approval event for node publish_gate exists;
# 2. no node_running event for node publish precedes it;
# 3. with --decision approve: a gate_verdict event for publish_gate with
#    verdict approve, then node_succeeded for publish, and the final
#    status_changed carries done;
# 4. with --decision reject: a gate_verdict with verdict reject and the
#    final status_changed carries blocked, with NO node_running for publish;
# 5. exit non-zero with a one-line reason on the first violated assert.

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

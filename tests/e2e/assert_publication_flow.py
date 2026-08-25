#!/usr/bin/env python3
"""Validate Batuta's automatic publication flow from public Loop events."""

import argparse
import json
import sys
from typing import NamedTuple
from urllib.parse import urlparse


class ValidationResult(NamedTuple):
    final_status: str
    verify_sequence: int = 0
    recovery_sequence: int = 0


def content(event: dict) -> dict:
    value = event.get("content", {})
    return value if isinstance(value, dict) else {}


def sequence(event: dict) -> int:
    value = event.get("sequence")
    assert isinstance(value, int), f"event has invalid sequence: {value!r}"
    return value


def ordered(events: list[dict]) -> list[dict]:
    assert isinstance(events, list), "loop-run events must be a JSON list"
    return sorted(events, key=sequence)


def node_id(event: dict) -> str | None:
    value = content(event).get("node_id")
    return value if isinstance(value, str) else None


def events_of_type(events: list[dict], event_type: str) -> list[dict]:
    return [event for event in events if event.get("type") == event_type]


def node_events(events: list[dict], event_type: str, expected_node: str) -> list[dict]:
    return [event for event in events_of_type(events, event_type) if node_id(event) == expected_node]


def final_status(events: list[dict]) -> str:
    changes = events_of_type(events, "status_changed")
    assert changes, "missing final status_changed event"
    value = content(changes[-1]).get("status")
    assert isinstance(value, str), "final status_changed has no status"
    return value


def details(event: dict) -> dict:
    value = content(event).get("details", {})
    return value if isinstance(value, dict) else {}


def is_https_url(value: object) -> bool:
    if not isinstance(value, str):
        return False
    parsed = urlparse(value)
    return parsed.scheme == "https" and bool(parsed.netloc) and not parsed.username and not parsed.password


def assert_no_healthy_approval(events: list[dict]) -> None:
    approvals = events_of_type(events, "needs_approval")
    assert not approvals, (
        f"healthy publication emitted needs_approval for {node_id(approvals[0])!r}"
    )


def validate_published(events: list[dict]) -> ValidationResult:
    assert_no_healthy_approval(events)
    successes = node_events(events, "node_succeeded", "publish") + node_events(
        events, "node_succeeded", "publish_after_recovery"
    )
    assert successes, "missing node_succeeded for publish"
    published = successes[-1]
    evidence = details(published)
    assert "compare_url" not in evidence, "compare-only publication evidence is forbidden"
    assert is_https_url(evidence.get("pr_url")), "published result requires a real HTTPS PR URL"
    head = evidence.get("head_sha")
    assert isinstance(head, str) and bool(head), "published result requires head_sha"
    operation_ids = evidence.get("op_ids")
    assert isinstance(operation_ids, list) and bool(operation_ids), "published result requires operation IDs"

    verifications = node_events(events, "node_succeeded", "publication_verify") + node_events(
        events, "node_succeeded", "publication_verify_after_recovery"
    )
    verifications = [event for event in verifications if sequence(event) > sequence(published)]
    assert verifications, "missing publication_verify after publish"
    verified = details(verifications[-1])
    assert verified.get("verified") is True and verified.get("status") == "published", (
        f"publication_verify did not confirm published result: {verified!r}"
    )
    assert verified.get("head_sha") == head and verified.get("pr_url") == evidence.get("pr_url"), (
        "publication_verify evidence does not match publisher evidence"
    )
    status = final_status(events)
    assert status == "done", f"final status = {status!r}, want done"
    return ValidationResult(final_status=status, verify_sequence=sequence(verifications[-1]))


def validate_nothing(events: list[dict]) -> ValidationResult:
    assert_no_healthy_approval(events)
    publish_events = [
        event
        for event in events
        if node_id(event) in {"publish", "publish_after_recovery"}
        and event.get("type") in {"node_running", "node_succeeded"}
    ]
    assert not publish_events, "nothing_to_publish path ran publisher"
    verifications = node_events(events, "node_succeeded", "publication_verify_nothing") + node_events(
        events, "node_succeeded", "publication_verify_nothing_after_recovery"
    )
    assert verifications, "missing nothing_to_publish verification"
    verified = details(verifications[-1])
    assert verified.get("verified") is True and verified.get("status") == "nothing_to_publish", (
        f"no-op verifier evidence = {verified!r}"
    )
    status = final_status(events)
    assert status == "done", f"final status = {status!r}, want done"
    return ValidationResult(final_status=status, verify_sequence=sequence(verifications[-1]))


def validate_blocked(events: list[dict]) -> ValidationResult:
    approvals = events_of_type(events, "needs_approval")
    recovery = [event for event in approvals if node_id(event) == "recovery_gate"]
    assert recovery, "missing needs_approval for recovery_gate"
    assert len(approvals) == len(recovery), "non-recovery human gate observed"
    recovery_sequence = sequence(recovery[0])
    for event in events:
        if node_id(event) in {"publish", "publish_after_recovery"} and sequence(event) < recovery_sequence:
            assert event.get("type") not in {"node_running", "node_succeeded"}, (
                "publish ran before recovery_gate"
            )
    status = final_status(events)
    assert status in {"needs-approval", "blocked"}, (
        f"blocked flow final status = {status!r}"
    )
    return ValidationResult(final_status=status, recovery_sequence=recovery_sequence)


def validate_flow(events: list[dict], scenario: str) -> ValidationResult:
    events = ordered(events)
    if scenario == "published":
        return validate_published(events)
    if scenario == "nothing_to_publish":
        return validate_nothing(events)
    if scenario == "blocked":
        return validate_blocked(events)
    raise AssertionError(f"unsupported publication scenario: {scenario!r}")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--events", required=True)
    parser.add_argument(
        "--scenario", required=True, choices=["published", "nothing_to_publish", "blocked"]
    )
    args = parser.parse_args()
    with open(args.events, encoding="utf-8") as handle:
        events = json.load(handle)
    result = validate_flow(events, args.scenario)
    print(
        "OK: automatic publication flow "
        f"scenario={args.scenario} final_status={result.final_status} "
        f"verify_sequence={result.verify_sequence} recovery_sequence={result.recovery_sequence}"
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (AssertionError, OSError, json.JSONDecodeError) as error:
        print(f"publication flow violation: {error}", file=sys.stderr)
        raise SystemExit(1) from error

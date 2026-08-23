#!/usr/bin/env python3
"""Regression coverage for the publication gate's public event ordering contract."""

import importlib.util
from pathlib import Path
import unittest


MODULE_PATH = Path(__file__).with_name("assert_publication_gate.py")
SPEC = importlib.util.spec_from_file_location("assert_publication_gate", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
validator = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(validator)


def event(event_type: str, sequence: int, **fields) -> dict:
    return {"type": event_type, "sequence": sequence, "content": dict(fields)}


def needs_approval(sequence: int, node_id: str = "publish_gate") -> dict:
    return event("needs_approval", sequence, node_id=node_id)


def gate_verdict(
    sequence: int,
    verdict: str,
    node_id: str = "publish_gate",
    generation: int = 1,
    gate_id: str = "gate-1",
    reason: str = "looks good",
    route: str | None = None,
) -> dict:
    fields = {
        "node_id": node_id,
        "generation": generation,
        "gate_id": gate_id,
        "verdict": verdict,
        "reason": reason,
    }
    if route is not None:
        fields["route"] = route
    return event("gate_verdict", sequence, **fields)


def node_running(sequence: int, node_id: str) -> dict:
    return event("node_running", sequence, node_id=node_id)


def node_succeeded(sequence: int, node_id: str, details: dict | None = None) -> dict:
    fields: dict = {"node_id": node_id}
    if details is not None:
        fields["details"] = details
    return event("node_succeeded", sequence, **fields)


def publish_evidence(
    head_sha: str = "deadbeefcafef00d",
    pr_url: str | None = "https://github.com/example/repo/pull/42",
    compare_url: str | None = None,
) -> dict:
    evidence: dict = {"head_sha": head_sha}
    if pr_url is not None:
        evidence["pr_url"] = pr_url
    if compare_url is not None:
        evidence["compare_url"] = compare_url
    return evidence


def node_failed(sequence: int, node_id: str) -> dict:
    return event("node_failed", sequence, node_id=node_id)


def status_changed(sequence: int, status: str) -> dict:
    return event("status_changed", sequence, status=status)


def approve_events() -> list[dict]:
    return [
        needs_approval(1),
        gate_verdict(2, "approve"),
        node_running(3, "publish"),
        node_succeeded(4, "publish", details=publish_evidence()),
        status_changed(5, "done"),
    ]


def approve_events_without_evidence() -> list[dict]:
    return [
        needs_approval(1),
        gate_verdict(2, "approve"),
        node_running(3, "publish"),
        node_succeeded(4, "publish"),
        status_changed(5, "done"),
    ]


def reject_events() -> list[dict]:
    return [
        needs_approval(1),
        gate_verdict(2, "reject"),
        status_changed(3, "blocked"),
    ]


class ApproveDecisionTests(unittest.TestCase):
    def test_accepts_a_clean_approve_run(self) -> None:
        result = validator.validate_gate(approve_events(), "approve")

        self.assertEqual(result.gate_park_sequence, 1)
        self.assertEqual(result.verdict_sequence, 2)
        self.assertEqual(result.final_status_sequence, 5)
        self.assertEqual(result.final_status, "done")

    def test_rejects_missing_needs_approval_for_publish_gate(self) -> None:
        events = [
            gate_verdict(2, "approve"),
            node_running(3, "publish"),
            node_succeeded(4, "publish"),
            status_changed(5, "done"),
        ]

        with self.assertRaisesRegex(AssertionError, r"needs_approval.*publish_gate"):
            validator.validate_gate(events, "approve")

    def test_rejects_node_running_publish_that_precedes_the_gate_park(self) -> None:
        events = [
            node_running(1, "publish"),
            needs_approval(2),
            gate_verdict(3, "approve"),
            node_succeeded(4, "publish"),
            status_changed(5, "done"),
        ]

        with self.assertRaisesRegex(
            AssertionError, r"node_running.*publish.*sequence 1.*precedes.*sequence 2"
        ):
            validator.validate_gate(events, "approve")

    def test_rejects_missing_approve_gate_verdict(self) -> None:
        events = [
            needs_approval(1),
            gate_verdict(2, "reject"),
            node_running(3, "publish"),
            node_succeeded(4, "publish"),
            status_changed(5, "done"),
        ]

        with self.assertRaisesRegex(AssertionError, r"gate_verdict.*approve"):
            validator.validate_gate(events, "approve")

    def test_rejects_missing_node_succeeded_for_publish(self) -> None:
        events = [
            needs_approval(1),
            gate_verdict(2, "approve"),
            node_running(3, "publish"),
            status_changed(4, "done"),
        ]

        with self.assertRaisesRegex(AssertionError, r"node_succeeded.*publish"):
            validator.validate_gate(events, "approve")

    def test_accepts_an_approve_run_with_publication_evidence(self) -> None:
        result = validator.validate_gate(approve_events(), "approve")

        self.assertEqual(result.final_status, "done")

    def test_rejects_an_approve_run_with_no_publication_evidence(self) -> None:
        with self.assertRaisesRegex(AssertionError, r"missing head_sha.*'details'.*publish"):
            validator.validate_gate(approve_events_without_evidence(), "approve")

    def test_rejects_publication_evidence_missing_head_sha(self) -> None:
        events = [
            needs_approval(1),
            gate_verdict(2, "approve"),
            node_running(3, "publish"),
            node_succeeded(
                4, "publish", details={"pr_url": "https://github.com/example/repo/pull/42"}
            ),
            status_changed(5, "done"),
        ]

        with self.assertRaisesRegex(AssertionError, r"missing head_sha.*'details'.*publish"):
            validator.validate_gate(events, "approve")

    def test_rejects_publication_evidence_missing_any_url(self) -> None:
        events = [
            needs_approval(1),
            gate_verdict(2, "approve"),
            node_running(3, "publish"),
            node_succeeded(4, "publish", details={"head_sha": "deadbeefcafef00d"}),
            status_changed(5, "done"),
        ]

        with self.assertRaisesRegex(
            AssertionError, r"missing publication URL \(pr_url or compare_url\).*publish"
        ):
            validator.validate_gate(events, "approve")

    def test_accepts_publication_evidence_with_compare_url_only(self) -> None:
        events = [
            needs_approval(1),
            gate_verdict(2, "approve"),
            node_running(3, "publish"),
            node_succeeded(
                4,
                "publish",
                details=publish_evidence(
                    pr_url=None, compare_url="https://github.com/example/repo/compare/main...batuta/x"
                ),
            ),
            status_changed(5, "done"),
        ]

        result = validator.validate_gate(events, "approve")
        self.assertEqual(result.final_status, "done")

    def test_rejects_final_status_that_is_not_done(self) -> None:
        events = [
            needs_approval(1),
            gate_verdict(2, "approve"),
            node_running(3, "publish"),
            node_succeeded(4, "publish"),
            status_changed(5, "blocked"),
        ]

        with self.assertRaisesRegex(AssertionError, r"final status_changed.*blocked.*done"):
            validator.validate_gate(events, "approve")


class RejectDecisionTests(unittest.TestCase):
    def test_accepts_a_clean_reject_run(self) -> None:
        result = validator.validate_gate(reject_events(), "reject")

        self.assertEqual(result.gate_park_sequence, 1)
        self.assertEqual(result.verdict_sequence, 2)
        self.assertEqual(result.final_status_sequence, 3)
        self.assertEqual(result.final_status, "blocked")

    def test_rejects_missing_reject_gate_verdict(self) -> None:
        events = [
            needs_approval(1),
            gate_verdict(2, "approve"),
            status_changed(3, "blocked"),
        ]

        with self.assertRaisesRegex(AssertionError, r"gate_verdict.*reject"):
            validator.validate_gate(events, "reject")

    def test_rejects_final_status_that_is_not_blocked(self) -> None:
        events = [
            needs_approval(1),
            gate_verdict(2, "reject"),
            status_changed(3, "done"),
        ]

        with self.assertRaisesRegex(AssertionError, r"final status_changed.*done.*blocked"):
            validator.validate_gate(events, "reject")

    def test_rejects_any_node_running_for_publish_after_a_reject(self) -> None:
        events = [
            needs_approval(1),
            gate_verdict(2, "reject"),
            node_running(3, "publish"),
            node_failed(4, "publish"),
            status_changed(5, "blocked"),
        ]

        with self.assertRaisesRegex(AssertionError, r"node_running.*publish.*reject"):
            validator.validate_gate(events, "reject")


if __name__ == "__main__":
    unittest.main()

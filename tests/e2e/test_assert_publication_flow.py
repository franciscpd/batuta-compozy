#!/usr/bin/env python3
"""Regression coverage for Batuta's automatic publication event contract."""

import importlib.util
from pathlib import Path
import unittest


MODULE_PATH = Path(__file__).with_name("assert_publication_flow.py")
SPEC = importlib.util.spec_from_file_location("assert_publication_flow", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
validator = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(validator)


def event(event_type: str, sequence: int, **fields) -> dict:
    return {"type": event_type, "sequence": sequence, "content": dict(fields)}


def published_events() -> list[dict]:
    return [
        event("node_running", 1, node_id="publication_plan"),
        event("node_succeeded", 2, node_id="publication_plan"),
        event("node_running", 3, node_id="publish"),
        event(
            "node_succeeded",
            4,
            node_id="publish",
            details={
                "head_sha": "a" * 40,
                "pr_url": "https://github.com/example/repo/pull/42",
                "op_ids": ["op-push", "op-pr"],
            },
        ),
        event(
            "node_succeeded",
            5,
            node_id="publication_verify",
            details={
                "verified": True,
                "status": "published",
                "head_sha": "a" * 40,
                "pr_url": "https://github.com/example/repo/pull/42",
            },
        ),
        event("status_changed", 6, status="done"),
    ]


class PublishedFlowTests(unittest.TestCase):
    def test_accepts_automatic_publish_then_independent_verify(self) -> None:
        result = validator.validate_flow(published_events(), "published")

        self.assertEqual(result.final_status, "done")
        self.assertEqual(result.verify_sequence, 5)

    def test_rejects_any_healthy_path_approval(self) -> None:
        events = published_events()
        events.insert(2, event("needs_approval", 2, node_id="publish_gate"))
        for index, item in enumerate(events, start=1):
            item["sequence"] = index

        with self.assertRaisesRegex(AssertionError, "healthy publication.*needs_approval"):
            validator.validate_flow(events, "published")

    def test_rejects_compare_only_evidence(self) -> None:
        events = published_events()
        details = events[3]["content"]["details"]
        details.pop("pr_url")
        details["compare_url"] = "https://github.com/example/repo/compare/main...batuta/x"

        with self.assertRaisesRegex(AssertionError, "compare-only"):
            validator.validate_flow(events, "published")

    def test_rejects_publish_without_later_verification(self) -> None:
        events = [event for event in published_events() if event["content"].get("node_id") != "publication_verify"]

        with self.assertRaisesRegex(AssertionError, "publication_verify"):
            validator.validate_flow(events, "published")


class NothingToPublishFlowTests(unittest.TestCase):
    def test_accepts_verified_noop_without_publisher_or_gate(self) -> None:
        events = [
            event("node_succeeded", 1, node_id="publication_plan"),
            event(
                "node_succeeded",
                2,
                node_id="publication_verify_nothing",
                details={"verified": True, "status": "nothing_to_publish", "head_sha": "a" * 40},
            ),
            event("status_changed", 3, status="done"),
        ]

        result = validator.validate_flow(events, "nothing_to_publish")
        self.assertEqual(result.final_status, "done")


class BlockedFlowTests(unittest.TestCase):
    def test_accepts_only_recovery_gate_before_any_mutation(self) -> None:
        events = [
            event("node_succeeded", 1, node_id="publication_plan"),
            event("needs_approval", 2, node_id="recovery_gate"),
            event("status_changed", 3, status="needs-approval"),
        ]

        result = validator.validate_flow(events, "blocked")
        self.assertEqual(result.recovery_sequence, 2)

    def test_rejects_mutation_before_recovery_gate(self) -> None:
        events = [
            event("node_running", 1, node_id="publish"),
            event("needs_approval", 2, node_id="recovery_gate"),
            event("status_changed", 3, status="needs-approval"),
        ]

        with self.assertRaisesRegex(AssertionError, "publish ran before recovery_gate"):
            validator.validate_flow(events, "blocked")


if __name__ == "__main__":
    unittest.main()

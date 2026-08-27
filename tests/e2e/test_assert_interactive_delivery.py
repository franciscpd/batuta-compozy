#!/usr/bin/env python3
"""Pure fixture coverage for interactive Batuta delivery assertions."""

import copy
import unittest

from tests.e2e.assert_interactive_delivery import validate_interactive_delivery


def saved_event_fixture() -> dict:
    return {"events": [
        {"sequence": 1, "type": "sdd_clarification_opened", "clarification_id": "clarify-1", "state": "pending"},
        {"sequence": 2, "type": "sdd_clarification_settled", "clarification_id": "clarify-1", "state": "answered"},
        {"sequence": 3, "type": "sdd_clarification_opened", "clarification_id": "clarify-2", "state": "pending"},
        {"sequence": 4, "type": "sdd_clarification_settled", "clarification_id": "clarify-2", "state": "answered"},
        {"sequence": 5, "type": "task_request_opened", "request_id": "request-1", "state": "pending", "responder": "human", "task_id": "task_01", "worktree_id": "wt_01", "execution": 1},
        {"sequence": 6, "type": "task_activity", "task_id": "task_02", "worktree_id": "wt_02"},
        {"sequence": 7, "type": "task_request_answered", "request_id": "request-1", "state": "answered", "responder": "human", "task_id": "task_01", "worktree_id": "wt_01", "answer": "Preserve compatibility"},
        {"sequence": 8, "type": "task_context", "task_id": "task_01", "worktree_id": "wt_01", "execution": 2, "answer_from_request": "request-1", "answer": "Preserve compatibility"},
        {"sequence": 9, "type": "task_request_opened", "request_id": "request-2", "state": "pending", "responder": "human", "task_id": "task_03", "worktree_id": "wt_03", "execution": 1},
        {"sequence": 10, "type": "task_request_rejected", "request_id": "request-2", "state": "expired"},
    ]}


class InteractiveDeliveryFixtureTests(unittest.TestCase):
    def test_accepts_saved_interactive_delivery_fixture(self) -> None:
        validate_interactive_delivery(saved_event_fixture())

    def test_rejects_multiple_pending_clarifications(self) -> None:
        fixture = saved_event_fixture()
        fixture["events"][3]["type"] = "sdd_clarification_opened"
        with self.assertRaisesRegex(AssertionError, "more than one SDD clarification"):
            validate_interactive_delivery(fixture)

    def test_rejects_answer_with_wrong_task_or_guessed_expired_input(self) -> None:
        for mutate in (
            lambda fixture: fixture["events"][6].update(task_id="task_02"),
            lambda fixture: fixture["events"].append({"sequence": 11, "type": "task_context", "task_id": "task_03", "worktree_id": "wt_03", "execution": 2, "answer_from_request": "request-2", "answer": "guessed"}),
        ):
            with self.subTest(mutate=mutate):
                fixture = copy.deepcopy(saved_event_fixture())
                mutate(fixture)
                with self.assertRaises(AssertionError):
                    validate_interactive_delivery(fixture)


if __name__ == "__main__":
    unittest.main()

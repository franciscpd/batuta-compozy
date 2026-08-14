#!/usr/bin/env python3
"""Regression coverage for Batuta's public event ordering contract."""

import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch


MODULE_PATH = Path(__file__).with_name("assert_event_driven_return.py")
SPEC = importlib.util.spec_from_file_location("assert_event_driven_return", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
validator = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(validator)


RUN_ID = "looprun-delivery-123"
DISPATCH_TURN = "turn-dispatch"
TERMINAL_TURN = "turn-terminal"
PROGRESS_TURN = "turn-progress"


def tool_call(
    sequence: int,
    turn_id: str,
    tool: str,
    arguments: dict,
    tool_call_id: str,
) -> dict:
    return {
        "type": "tool_call",
        "sequence": sequence,
        "content": {
            "turn_id": turn_id,
            "tool_call_id": tool_call_id,
            "tool_input": {"tool": tool, "arguments": arguments},
        },
    }


def tool_result(
    sequence: int,
    turn_id: str,
    tool_call_id: str,
    structured_content: dict | None,
) -> dict:
    result = {"content": [], "structuredContent": structured_content}
    return {
        "type": "tool_result",
        "sequence": sequence,
        "content": {
            "turn_id": turn_id,
            "tool_call_id": tool_call_id,
            "tool_input": {"tool": "compozy__loop_run", "arguments": {}},
            "tool_result": {"raw_output": {"result": result}},
        },
    }


def text(sequence: int, turn_id: str, value: str, event_type: str = "agent_message") -> dict:
    return {
        "type": event_type,
        "sequence": sequence,
        "content": {
            "turn_id": turn_id,
            "tool_call_id": None,
            "tool_input": {"tool": None, "arguments": {}},
            "text": value,
        },
    }


def dispatch_call(sequence: int = 1, *, dry: bool | None = None) -> dict:
    arguments = {"name": "batuta-deliver"}
    if dry is not None:
        arguments["dry"] = dry
    return tool_call(sequence, DISPATCH_TURN, "compozy__loop_run", arguments, "call-dispatch")


def accepted_dispatch() -> list[dict]:
    return [
        dispatch_call(),
        tool_result(2, DISPATCH_TURN, "call-dispatch", {"run": {"id": RUN_ID}}),
    ]


def terminal_prompt(sequence: int = 5, turn_id: str = TERMINAL_TURN) -> dict:
    return text(
        sequence,
        turn_id,
        f"Batuta delivery run {RUN_ID} reached terminal\ntrigger done",
        "user_message",
    )


def matching_status(sequence: int, turn_id: str, call_id: str = "call-status") -> dict:
    return tool_call(
        sequence,
        turn_id,
        "compozy__loop_status",
        {"run_id": RUN_ID},
        call_id,
    )


class ValidateDeliveryTests(unittest.TestCase):
    def test_rejects_status_after_accepted_dispatch_in_the_same_turn(self) -> None:
        events = accepted_dispatch() + [matching_status(3, DISPATCH_TURN)]

        with self.assertRaisesRegex(
            AssertionError,
            r"accepted result at sequence 2.*tool call at sequence 3.*turn-dispatch",
        ):
            validator.validate_delivery(events, RUN_ID)

    def test_rejects_shell_sleep_after_accepted_dispatch_in_the_same_turn(self) -> None:
        events = accepted_dispatch() + [
            tool_call(3, DISPATCH_TURN, "shell", {"command": "sleep 15"}, "call-sleep")
        ]

        with self.assertRaisesRegex(AssertionError, r"sequence 2.*sequence 3"):
            validator.validate_delivery(events, RUN_ID)

    def test_dry_run_and_failed_real_submission_do_not_create_a_boundary(self) -> None:
        events = [
            dispatch_call(1, dry=True),
            tool_result(2, DISPATCH_TURN, "call-dispatch", {"run": {"id": RUN_ID}}),
            tool_call(
                3,
                DISPATCH_TURN,
                "compozy__loop_run",
                {"name": "batuta-deliver"},
                "call-real",
            ),
            tool_result(4, DISPATCH_TURN, "call-real", None),
            matching_status(5, DISPATCH_TURN),
        ]

        with self.assertRaisesRegex(AssertionError, r"accepted batuta-deliver result"):
            validator.validate_delivery(events, RUN_ID)

    def test_accepts_later_terminal_prompt_and_matching_first_status_read(self) -> None:
        events = accepted_dispatch() + [
            text(3, DISPATCH_TURN, "Delivery accepted."),
            text(4, DISPATCH_TURN, "done", "done"),
            text(5, TERMINAL_TURN, f"Batuta delivery run {RUN_ID} ", "user_message"),
            text(6, TERMINAL_TURN, "reached terminal\ntrigger done", "user_message"),
            matching_status(7, TERMINAL_TURN),
        ]

        result = validator.validate_delivery(events, RUN_ID)

        self.assertEqual(
            result,
            validator.ValidationResult(2, DISPATCH_TURN, 5, 7),
        )

    def test_rejects_terminal_status_that_precedes_the_terminal_prompt(self) -> None:
        events = accepted_dispatch() + [
            matching_status(3, TERMINAL_TURN),
            terminal_prompt(5, TERMINAL_TURN),
        ]

        with self.assertRaisesRegex(
            AssertionError,
            r"terminal prompt at sequence 5.*after the terminal prompt",
        ):
            validator.validate_delivery(events, RUN_ID)

    def test_rejects_terminal_turn_starting_with_another_tool(self) -> None:
        events = accepted_dispatch() + [
            terminal_prompt(),
            tool_call(6, TERMINAL_TURN, "shell", {"command": "true"}, "call-shell"),
            matching_status(7, TERMINAL_TURN),
        ]

        with self.assertRaisesRegex(AssertionError, r"first tool call.*shell.*call-shell"):
            validator.validate_delivery(events, RUN_ID)

    def test_rejects_terminal_status_for_a_different_run(self) -> None:
        events = accepted_dispatch() + [
            terminal_prompt(),
            tool_call(
                6,
                TERMINAL_TURN,
                "compozy__loop_status",
                {"run_id": "looprun-other"},
                "call-status",
            ),
        ]

        with self.assertRaisesRegex(AssertionError, r"looprun-other.*looprun-delivery-123"):
            validator.validate_delivery(events, RUN_ID)

    def test_rejects_duplicate_terminal_prompts_for_the_same_run(self) -> None:
        events = accepted_dispatch() + [
            terminal_prompt(5, TERMINAL_TURN),
            matching_status(6, TERMINAL_TURN),
            terminal_prompt(7, "turn-terminal-duplicate"),
            matching_status(8, "turn-terminal-duplicate", "call-status-duplicate"),
        ]

        with self.assertRaisesRegex(AssertionError, r"duplicate terminal prompts.*5.*7"):
            validator.validate_delivery(events, RUN_ID)

    def test_rejects_a_truncated_event_window(self) -> None:
        events = accepted_dispatch()
        for event in events:
            event["sequence"] += 1

        with self.assertRaisesRegex(AssertionError, r"first sequence 2.*complete ordering evidence"):
            validator.validate_delivery(events, RUN_ID)


class ValidateProgressTurnTests(unittest.TestCase):
    def test_accepts_exactly_one_matching_status_read(self) -> None:
        events = [matching_status(1, PROGRESS_TURN)]

        self.assertEqual(validator.validate_progress_turn(events, RUN_ID, PROGRESS_TURN), 1)

    def test_rejects_two_status_reads_or_a_wait_tool(self) -> None:
        with self.subTest("two status reads"):
            events = [
                matching_status(1, PROGRESS_TURN),
                matching_status(2, PROGRESS_TURN, "call-status-second"),
            ]
            with self.assertRaisesRegex(AssertionError, r"exactly one tool call"):
                validator.validate_progress_turn(events, RUN_ID, PROGRESS_TURN)

        with self.subTest("wait tool"):
            events = [tool_call(1, PROGRESS_TURN, "session_wait", {}, "call-wait")]
            with self.assertRaisesRegex(AssertionError, r"compozy__loop_status"):
                validator.validate_progress_turn(events, RUN_ID, PROGRESS_TURN)


class FetchEventsTests(unittest.TestCase):
    def test_requests_the_supported_complete_event_window(self) -> None:
        with patch.object(validator.subprocess, "run") as run:
            run.return_value.stdout = '[{"sequence": 1}]'

            events = validator.fetch_events("compozy", "sess-123")

        self.assertEqual(events, [{"sequence": 1}])
        self.assertEqual(
            run.call_args.args[0],
            [
                "compozy",
                "session",
                "events",
                "sess-123",
                "--archive",
                "all",
                "--last",
                "1000",
                "-o",
                "json",
            ],
        )

if __name__ == "__main__":
    unittest.main()

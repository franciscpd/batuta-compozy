#!/usr/bin/env python3
"""Regression coverage for Batuta's delivery-path preference gate ordering."""

import importlib.util
from pathlib import Path
import unittest


MODULE_PATH = Path(__file__).with_name("assert_preference_gate.py")
SPEC = importlib.util.spec_from_file_location("assert_preference_gate", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
validator = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(validator)


TURN = "turn-1"


def tool_call(sequence: int, tool: str, arguments: dict, call_id: str) -> dict:
    return {
        "type": "tool_call",
        "sequence": sequence,
        "content": {
            "turn_id": TURN,
            "tool_call_id": call_id,
            "tool_input": {"tool": tool, "arguments": arguments},
        },
    }


def tool_result(sequence: int, call_id: str, structured_content: dict | None) -> dict:
    result = {"content": [], "structuredContent": structured_content}
    return {
        "type": "tool_result",
        "sequence": sequence,
        "content": {
            "turn_id": TURN,
            "tool_call_id": call_id,
            "tool_result": {"raw_output": {"result": result}},
        },
    }


def missing_result(sequence: int, call_id: str) -> dict:
    result = {
        "content": [],
        "isError": True,
        "error": {"code": "config_path_not_found"},
    }
    return {
        "type": "tool_result",
        "sequence": sequence,
        "content": {
            "turn_id": TURN,
            "tool_call_id": call_id,
            "tool_result": {"raw_output": {"result": result}},
        },
    }


def config_get_call(sequence: int, call_id: str = "call-get") -> dict:
    return tool_call(
        sequence,
        "compozy__config_get",
        {"path": validator.PREFERENCE_PATH, "workspace": True},
        call_id,
    )


def config_set_call(sequence: int, value: bool, call_id: str = "call-set") -> dict:
    return tool_call(
        sequence,
        "compozy__config_set",
        {
            "path": validator.PREFERENCE_PATH,
            "scope": "workspace",
            "workspace": True,
            "value": value,
        },
        call_id,
    )


def conversational_call(sequence: int, call_id: str = "call-status") -> dict:
    """A purely conversational tool call — e.g. a status read — with no config read."""
    return tool_call(sequence, "compozy__loop_status", {"run_id": "looprun-x"}, call_id)


def import_tasks_call(sequence: int, call_id: str = "call-import") -> dict:
    return tool_call(
        sequence,
        "ext__spec_cycle__import_tasks",
        {"pattern": ".compozy/tasks/demo/task_*.md"},
        call_id,
    )


def dispatch_call(sequence: int, call_id: str = "call-dispatch") -> dict:
    return tool_call(sequence, "compozy__loop_run", {"name": "batuta-deliver"}, call_id)


class ValidatePreferenceGateTests(unittest.TestCase):
    def test_accepts_a_conversational_turn_before_the_gate(self) -> None:
        events = [
            conversational_call(1),
            tool_result(2, "call-status", {"status": "running"}),
            config_get_call(3),
            tool_result(4, "call-get", {"entry": {"path": validator.PREFERENCE_PATH, "value": True}}),
            import_tasks_call(5),
        ]

        first, persisted, confirmed = validator.validate(events, True)

        self.assertEqual((first, persisted, confirmed), (3, 3, 4))

    def test_rejects_a_delivery_path_call_before_the_gate(self) -> None:
        events = [
            import_tasks_call(1),
            config_get_call(2),
            tool_result(3, "call-get", {"entry": {"path": validator.PREFERENCE_PATH, "value": True}}),
        ]

        with self.assertRaisesRegex(
            AssertionError,
            r"does not precede the first delivery-path call",
        ):
            validator.validate(events, True)

    def test_rejects_a_dispatch_before_the_persist_and_reread_completes(self) -> None:
        events = [
            config_get_call(1),
            missing_result(2, "call-get"),
            dispatch_call(3, "call-dispatch"),
            config_set_call(4, False),
            tool_result(5, "call-set", None),
            config_get_call(6),
            tool_result(
                7,
                "call-get",
                {"entry": {"path": validator.PREFERENCE_PATH, "value": False}},
            ),
        ]

        with self.assertRaisesRegex(
            AssertionError,
            r"tool 'compozy__loop_run'.*intervened",
        ):
            validator.validate(events, False)

    def test_accepts_persist_and_reread_with_delivery_call_after(self) -> None:
        events = [
            config_get_call(1, "call-get"),
            missing_result(2, "call-get"),
            config_set_call(3, False, "call-set"),
            tool_result(4, "call-set", None),
            config_get_call(5, "call-get2"),
            tool_result(
                6,
                "call-get2",
                {"entry": {"path": validator.PREFERENCE_PATH, "value": False}},
            ),
            dispatch_call(7, "call-dispatch"),
        ]

        first, persisted, confirmed = validator.validate(events, False)

        self.assertEqual((first, persisted, confirmed), (1, 3, 6))

    def test_rejects_a_delivery_call_that_precedes_the_final_reread_result(self) -> None:
        # Dispatched right after the confirmation reread's tool call, but
        # before that reread's own result lands — the gate has not actually
        # confirmed the reread value yet.
        events = [
            config_get_call(1, "call-get"),
            missing_result(2, "call-get"),
            config_set_call(3, False, "call-set"),
            tool_result(4, "call-set", None),
            config_get_call(5, "call-get2"),
            dispatch_call(6, "call-dispatch"),
            tool_result(
                7,
                "call-get2",
                {"entry": {"path": validator.PREFERENCE_PATH, "value": False}},
            ),
        ]

        with self.assertRaisesRegex(
            AssertionError,
            r"delivery-path call 'compozy__loop_run' at sequence 6 precedes the "
            r"gate's final reread at sequence 7",
        ):
            validator.validate(events, False)


if __name__ == "__main__":
    unittest.main()

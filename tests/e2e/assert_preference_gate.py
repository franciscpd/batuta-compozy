#!/usr/bin/env python3
"""Validate Batuta's auto_commit gate from public session events."""

import argparse
import json
import subprocess
import sys


PREFERENCE_PATH = "loops.inputs.batuta-deliver.auto_commit"


def parse_boolean(value: str) -> bool:
    if value == "true":
        return True
    if value == "false":
        return False
    raise argparse.ArgumentTypeError("expected true or false")


def tool_name(event: dict) -> str | None:
    return event.get("content", {}).get("tool_input", {}).get("tool")


def arguments(event: dict) -> dict:
    return event.get("content", {}).get("tool_input", {}).get("arguments", {})


def structured_result(event: dict) -> dict | None:
    return (
        event.get("content", {})
        .get("tool_result", {})
        .get("raw_output", {})
        .get("result", {})
        .get("structuredContent")
    )


def find_result(events: list[dict], call: dict) -> dict:
    call_id = call.get("content", {}).get("tool_call_id")
    for event in events:
        content = event.get("content", {})
        if event.get("type") == "tool_result" and content.get("tool_call_id") == call_id:
            return event
    raise AssertionError(f"tool call at sequence {call['sequence']} has no result")


def validate(events: list[dict], expected: bool) -> tuple[int, int, int]:
    calls = [event for event in events if event.get("type") == "tool_call"]
    if not calls:
        raise AssertionError("session contains no tool calls")

    initial_get = calls[0]
    initial_args = arguments(initial_get)
    assert tool_name(initial_get) == "compozy__config_get", (
        f"first tool call was {tool_name(initial_get)!r} at sequence "
        f"{initial_get['sequence']}, expected compozy__config_get"
    )
    assert initial_args.get("path") == PREFERENCE_PATH, initial_args
    assert initial_args.get("workspace"), "initial config_get is not workspace-bound"

    initial_result = find_result(events, initial_get)
    initial_structured = structured_result(initial_result)
    if initial_structured is not None:
        entry = initial_structured.get("entry", {})
        assert entry.get("path") == PREFERENCE_PATH, entry
        assert isinstance(entry.get("value"), bool), entry
        assert entry["value"] is expected, entry
        return initial_get["sequence"], initial_get["sequence"], initial_result["sequence"]

    missing_result = json.dumps(initial_result, sort_keys=True)
    assert "config_path_not_found" in missing_result, missing_result

    later_calls = [call for call in calls if call["sequence"] > initial_get["sequence"]]
    assert later_calls, "missing preference was not persisted"
    set_call = later_calls[0]
    set_args = arguments(set_call)
    assert tool_name(set_call) == "compozy__config_set", (
        f"tool {tool_name(set_call)!r} at sequence {set_call['sequence']} intervened "
        "between the missing read and config_set"
    )
    assert set_args.get("path") == PREFERENCE_PATH, set_args
    assert set_args.get("scope") == "workspace", set_args
    assert set_args.get("workspace") == initial_args["workspace"], set_args
    assert isinstance(set_args.get("value"), bool), set_args
    assert set_args["value"] is expected, set_args

    set_index = calls.index(set_call)
    assert set_index + 1 < len(calls), "config_set was not confirmed by a reread"
    confirm_call = calls[set_index + 1]
    confirm_args = arguments(confirm_call)
    assert tool_name(confirm_call) == "compozy__config_get", (
        f"tool {tool_name(confirm_call)!r} at sequence {confirm_call['sequence']} "
        "intervened between config_set and confirmation reread"
    )
    assert confirm_args == initial_args, (confirm_args, initial_args)

    confirm_result = find_result(events, confirm_call)
    confirm_structured = structured_result(confirm_result)
    assert confirm_structured is not None, confirm_result
    entry = confirm_structured.get("entry", {})
    assert entry.get("path") == PREFERENCE_PATH, entry
    assert isinstance(entry.get("value"), bool), entry
    assert entry["value"] is expected, entry

    return initial_get["sequence"], set_call["sequence"], confirm_result["sequence"]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--compozy", required=True)
    parser.add_argument("--session", required=True)
    parser.add_argument("--expected", required=True, type=parse_boolean)
    args = parser.parse_args()

    completed = subprocess.run(
        [args.compozy, "session", "events", args.session, "-o", "json"],
        check=True,
        capture_output=True,
        text=True,
    )
    events = json.loads(completed.stdout)
    first, persisted, confirmed = validate(events, args.expected)
    print(
        "OK: auto_commit gate order "
        f"first_get={first} persisted={persisted} confirmed={confirmed} "
        f"value={str(args.expected).lower()}"
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (AssertionError, subprocess.CalledProcessError, json.JSONDecodeError) as error:
        print(f"preference gate violation: {error}", file=sys.stderr)
        raise SystemExit(1) from error

#!/usr/bin/env python3
"""Validate Batuta's event-driven delivery return from public session events."""

import argparse
from dataclasses import dataclass
import json
import subprocess
import sys


@dataclass(frozen=True)
class ValidationResult:
    dispatch_sequence: int
    dispatch_turn_id: str
    terminal_prompt_sequence: int
    terminal_status_sequence: int


def content(event: dict) -> dict:
    return event.get("content", {})


def sequence(event: dict) -> int:
    value = event.get("sequence")
    assert isinstance(value, int), f"event has invalid sequence: {value!r}"
    return value


def event_turn_id(event: dict) -> str | None:
    value = content(event).get("turn_id")
    return value if isinstance(value, str) else None


def tool_name(event: dict) -> str | None:
    value = content(event).get("tool_input", {}).get("tool")
    return value if isinstance(value, str) else None


def arguments(event: dict) -> dict:
    value = content(event).get("tool_input", {}).get("arguments", {})
    return value if isinstance(value, dict) else {}


def structured_result(event: dict) -> dict | None:
    value = (
        content(event)
        .get("tool_result", {})
        .get("raw_output", {})
        .get("result", {})
        .get("structuredContent")
    )
    return value if isinstance(value, dict) else None


def ordered_events(events: list[dict]) -> list[dict]:
    assert isinstance(events, list), "session events must be a JSON list"
    assert events, "session event window is empty"
    ordered = sorted(events, key=sequence)
    assert sequence(ordered[0]) == 1, (
        f"first sequence {sequence(ordered[0])} is not 1; "
        "complete ordering evidence is unavailable"
    )
    for previous, current in zip(ordered, ordered[1:]):
        assert sequence(previous) != sequence(current), (
            f"duplicate event sequence {sequence(current)}"
        )
    return ordered


def fetch_events(compozy: str, session_id: str) -> list[dict]:
    completed = subprocess.run(
        [
            compozy,
            "session",
            "events",
            session_id,
            "--archive",
            "all",
            "--last",
            "1000",
            "-o",
            "json",
        ],
        check=True,
        capture_output=True,
        text=True,
    )
    events = json.loads(completed.stdout)
    return ordered_events(events)


def result_for_call(events: list[dict], call: dict) -> dict | None:
    call_id = content(call).get("tool_call_id")
    assert isinstance(call_id, str), f"tool call at sequence {sequence(call)} has no tool_call_id"
    for event in events:
        if event.get("type") != "tool_result":
            continue
        if content(event).get("tool_call_id") == call_id:
            return event
    return None


def accepted_dispatch(events: list[dict], run_id: str) -> tuple[dict, dict]:
    for call in events:
        if event_type(call) != "tool_call":
            continue
        call_arguments = arguments(call)
        if tool_name(call) != "compozy__loop_run":
            continue
        if call_arguments.get("name") != "batuta-deliver":
            continue
        if call_arguments.get("dry", False) is not False:
            continue
        result = result_for_call(events, call)
        if result is None:
            continue
        structured = structured_result(result)
        if structured and structured.get("run", {}).get("id") == run_id:
            return call, result
    raise AssertionError(f"no accepted batuta-deliver result for run {run_id}")


def event_type(event: dict) -> str | None:
    value = event.get("type")
    return value if isinstance(value, str) else None


def terminal_prompts(events: list[dict], run_id: str, dispatch_turn: str, after: int) -> list[tuple[int, str]]:
    prefix = f"Batuta delivery run {run_id} reached terminal"
    fragments_by_turn: dict[str, list[dict]] = {}
    for event in events:
        if event_type(event) != "user_message" or sequence(event) <= after:
            continue
        event_turn = event_turn_id(event)
        if event_turn is None or event_turn == dispatch_turn:
            continue
        if isinstance(content(event).get("text"), str):
            fragments_by_turn.setdefault(event_turn, []).append(event)

    matches: list[tuple[int, str]] = []
    for event_turn, fragments in fragments_by_turn.items():
        message = "".join(content(fragment)["text"] for fragment in fragments)
        matches.extend((sequence(fragments[0]), event_turn) for _ in range(message.count(prefix)))
    return matches


def validate_delivery(events: list[dict], run_id: str) -> ValidationResult:
    events = ordered_events(events)
    dispatch, dispatch_result = accepted_dispatch(events, run_id)
    dispatch_turn = event_turn_id(dispatch)
    assert dispatch_turn is not None, f"dispatch call at sequence {sequence(dispatch)} has no turn_id"
    dispatch_result_sequence = sequence(dispatch_result)

    for event in events:
        if (
            event_type(event) == "tool_call"
            and event_turn_id(event) == dispatch_turn
            and sequence(event) > dispatch_result_sequence
        ):
            raise AssertionError(
                f"accepted result at sequence {dispatch_result_sequence}, later tool call "
                f"at sequence {sequence(event)} in turn {dispatch_turn}"
            )

    prompts = terminal_prompts(events, run_id, dispatch_turn, dispatch_result_sequence)
    assert prompts, f"missing terminal prompt for run {run_id}"
    if len(prompts) != 1:
        prompt_sequences = ", ".join(str(prompt[0]) for prompt in prompts)
        raise AssertionError(f"duplicate terminal prompts at sequences {prompt_sequences}")
    terminal_prompt_sequence, terminal_turn = prompts[0]

    terminal_calls = [
        event
        for event in events
        if event_type(event) == "tool_call" and event_turn_id(event) == terminal_turn
    ]
    assert terminal_calls, f"terminal prompt at sequence {terminal_prompt_sequence} has no tool call"
    first_call = terminal_calls[0]
    assert sequence(first_call) > terminal_prompt_sequence, (
        f"terminal prompt at sequence {terminal_prompt_sequence} requires its first tool call "
        f"at sequence {sequence(first_call)} to occur after the terminal prompt"
    )
    assert tool_name(first_call) == "compozy__loop_status", (
        f"first tool call after terminal prompt is {tool_name(first_call)!r} "
        f"at sequence {sequence(first_call)} ({content(first_call).get('tool_call_id')!r}); "
        "expected compozy__loop_status"
    )
    status_run_id = arguments(first_call).get("run_id")
    assert status_run_id == run_id, (
        f"terminal status run_id {status_run_id!r} does not match {run_id!r}"
    )

    return ValidationResult(
        dispatch_sequence=dispatch_result_sequence,
        dispatch_turn_id=dispatch_turn,
        terminal_prompt_sequence=terminal_prompt_sequence,
        terminal_status_sequence=sequence(first_call),
    )


def validate_progress_turn(events: list[dict], run_id: str, turn_id: str) -> int:
    events = ordered_events(events)
    calls = [
        event
        for event in events
        if event_type(event) == "tool_call" and event_turn_id(event) == turn_id
    ]
    assert len(calls) == 1, (
        f"progress turn {turn_id} must contain exactly one tool call; found {len(calls)}"
    )
    call = calls[0]
    assert tool_name(call) == "compozy__loop_status", (
        f"progress tool at sequence {sequence(call)} is {tool_name(call)!r}; "
        "expected compozy__loop_status"
    )
    status_run_id = arguments(call).get("run_id")
    assert status_run_id == run_id, (
        f"progress status run_id {status_run_id!r} does not match {run_id!r}"
    )
    return sequence(call)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--compozy", required=True)
    parser.add_argument("--session", required=True)
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--progress-turn")
    args = parser.parse_args()

    events = fetch_events(args.compozy, args.session)
    result = validate_delivery(events, args.run_id)
    output = (
        "OK: event-driven return "
        f"dispatch_sequence={result.dispatch_sequence} "
        f"dispatch_turn={result.dispatch_turn_id} "
        f"terminal_prompt_sequence={result.terminal_prompt_sequence} "
        f"terminal_status_sequence={result.terminal_status_sequence}"
    )
    if args.progress_turn:
        output += (
            f" progress_sequence={validate_progress_turn(events, args.run_id, args.progress_turn)}"
        )
    print(output)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (AssertionError, subprocess.CalledProcessError, json.JSONDecodeError) as error:
        print(f"event-driven return violation: {error}", file=sys.stderr)
        raise SystemExit(1) from error

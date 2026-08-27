#!/usr/bin/env python3
"""Validate saved public evidence for Batuta's interactive delivery path."""

from __future__ import annotations

import argparse
import json
from pathlib import Path


def ordered_events(fixture: dict) -> list[dict]:
    events = fixture.get("events")
    assert isinstance(events, list) and events, "fixture must contain non-empty events"
    ordered = sorted(events, key=lambda event: event.get("sequence", -1))
    for expected, event in enumerate(ordered, 1):
        assert event.get("sequence") == expected, f"event sequence is incomplete: {event!r}"
    return ordered


def validate_interactive_delivery(fixture: dict) -> None:
    events = ordered_events(fixture)
    pending_clarification: str | None = None
    requests: dict[str, dict] = {}
    answers: dict[str, dict] = {}
    rejected: set[str] = set()

    for event in events:
        kind = event.get("type")
        if kind == "sdd_clarification_opened":
            clarification_id = event.get("clarification_id")
            assert isinstance(clarification_id, str) and clarification_id
            assert pending_clarification is None, "more than one SDD clarification is pending"
            assert event.get("state") == "pending", "opened SDD clarification is not pending"
            pending_clarification = clarification_id
        elif kind == "sdd_clarification_settled":
            assert event.get("clarification_id") == pending_clarification, "settled wrong clarification"
            pending_clarification = None
        elif kind == "task_request_opened":
            request_id = event.get("request_id")
            assert isinstance(request_id, str) and request_id not in requests, "invalid task request identity"
            assert event.get("state") == "pending" and event.get("responder") == "human"
            assert isinstance(event.get("task_id"), str) and isinstance(event.get("worktree_id"), str)
            requests[request_id] = event
        elif kind == "task_request_answered":
            request_id = event.get("request_id")
            request = requests.get(request_id)
            assert request is not None, "answer has no parked request"
            assert event.get("state") == "answered" and event.get("responder") == "human"
            assert event.get("task_id") == request["task_id"] and event.get("worktree_id") == request["worktree_id"]
            assert isinstance(event.get("answer"), str) and event["answer"].strip()
            answers[request_id] = event
        elif kind == "task_request_rejected":
            request_id = event.get("request_id")
            assert request_id in requests, "rejection has no parked request"
            assert event.get("state") in {"expired", "canceled"}, "invalid rejected request state"
            rejected.add(request_id)

    assert pending_clarification is None, "an SDD clarification remains pending"
    assert answers, "fixture has no answered task request"
    for request_id, answer in answers.items():
        request = requests[request_id]
        next_contexts = [
            event
            for event in events
            if event.get("type") == "task_context"
            and event.get("answer_from_request") == request_id
        ]
        assert len(next_contexts) == 1, "answered request does not produce exactly one next task context"
        context = next_contexts[0]
        assert context.get("task_id") == request["task_id"] and context.get("worktree_id") == request["worktree_id"]
        assert context.get("execution") == request.get("execution", 1) + 1
        assert context.get("answer") == answer["answer"], "next execution did not use winning answer"

        sibling_events = [
            event
            for event in events
            if request["sequence"] < event["sequence"] < answer["sequence"]
            and event.get("task_id") != request["task_id"]
            and event.get("type") == "task_activity"
        ]
        assert sibling_events, "parked task did not allow sibling task activity"
    for request_id in rejected:
        assert not any(
            event.get("type") == "task_context" and event.get("answer_from_request") == request_id
            for event in events
        ), "expired or canceled answer became guessed task input"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("fixture", type=Path)
    args = parser.parse_args()
    validate_interactive_delivery(json.loads(args.fixture.read_text()))


if __name__ == "__main__":
    main()

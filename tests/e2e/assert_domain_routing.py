from __future__ import annotations


TERMINAL_ATTEMPT = "terminal"


def assert_domain_routing(
    evidence: dict,
    expected_generation: str,
    frontend_task_id: str,
    primary_runtime: tuple[str, str],
    fallback_runtime: tuple[str, str],
) -> None:
    journal = evidence["journal"]
    delivery_id = evidence["delivery_id"]
    assert journal["schema_version"] == 2
    generation = journal["generations"][expected_generation]
    frontend_cell = next(
        cell for cell in generation["cells"] if frontend_task_id in cell["task_ids"]
    )
    assert frontend_cell["selected"]["executor_id"] == "compozy"
    enrichment_ids = frontend_cell["selected"].get("enrichment_ids", [])
    assert enrichment_ids == sorted(set(enrichment_ids))
    assert not any(
        key in frontend_cell["selected"]
        for key in ("raw_output", "capabilities", "diagnostics", "credentials")
    )
    delivery = journal["deliveries"][delivery_id]
    assert delivery["delivery_id"] == delivery_id
    assert delivery["routing_generation_digest"] == expected_generation
    assert delivery["attempt_ceiling"] == 4
    assert delivery["token_ceiling"] == 1_000_000
    assert delivery["state"] == "done"
    assert delivery["workspace_id"]
    assert delivery["worktree_id"]
    assert delivery["worktree_root"]
    assert delivery["task_set_digest"]

    attempts = delivery["attempts"]
    assert len(attempts) == 2
    assert [attempt["attempt"] for attempt in attempts] == [1, 2]
    assert all(attempt["state"] == TERMINAL_ATTEMPT for attempt in attempts)
    assert len({attempt["run_id"] for attempt in attempts}) == 2
    assert len({attempt["operation_id"] for attempt in attempts}) == 2
    assert len({attempt["request_digest"] for attempt in attempts}) == 2

    first_rules = rules_by_task(attempts[0])
    second_rules = rules_by_task(attempts[1])
    assert runtime_pair(first_rules[frontend_task_id]) == primary_runtime
    assert set(second_rules) == {frontend_task_id}
    assert runtime_pair(second_rules[frontend_task_id]) == fallback_runtime
    assert attempts[0]["failed_task_ids"] == [frontend_task_id]

    child_ids = [child for attempt in attempts for child in attempt["child_run_ids"]]
    assert child_ids
    assert len(child_ids) == len(set(child_ids))

    observed = {
        (
            item["attempt"],
            item["run_id"],
            item["task_id"],
            item["provider"],
            item["model"],
        )
        for item in evidence["runtime_applied"]
    }
    expected = {
        (
            attempt["attempt"],
            attempt["run_id"],
            task_id,
            rule["runtime"]["provider"],
            rule["runtime"]["model"],
        )
        for attempt in attempts
        for task_id, rule in rules_by_task(attempt).items()
    }
    assert observed == expected
    assert evidence["stored_config_calls"] == 0
    assert evidence["external_start_calls"] == 2
    assert evidence["merge_attempts"] == 0
    assert evidence["publication"]["verified"] is True
    assert evidence["publication"]["head_sha"] == evidence["remote_head_sha"]


def rules_by_task(attempt: dict) -> dict[str, dict]:
    rules = attempt["runtime_rules"]
    by_task = {rule["match"]["id"]: rule for rule in rules}
    assert len(by_task) == len(rules)
    assert all(task_id for task_id in by_task)
    return by_task


def runtime_pair(rule: dict) -> tuple[str, str]:
    runtime = rule["runtime"]
    return runtime["provider"], runtime["model"]

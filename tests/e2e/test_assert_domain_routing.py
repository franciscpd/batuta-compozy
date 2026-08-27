import copy
import unittest

from tests.e2e.assert_domain_routing import assert_domain_routing


GENERATION = "sha256:" + "a" * 64
DELIVERY_ID = "sha256:" + "b" * 64
CURSOR = ("cursor", "grok-4.6[effort=high,fast=true]")
CODEX = ("codex", "gpt-5.6-terra")


def rule(task_id: str, runtime: tuple[str, str]) -> dict:
    return {
        "match": {"id": task_id},
        "runtime": {"provider": runtime[0], "model": runtime[1], "reasoning": "high"},
    }


def valid_evidence() -> dict:
    attempts = [
        {
            "attempt": 1,
            "operation_id": "sha256:" + "1" * 64,
            "request_digest": "sha256:" + "2" * 64,
            "state": "terminal",
            "run_id": "run_attempt_1",
            "child_run_ids": ["implement_1"],
            "failed_task_ids": ["task_02"],
            "runtime_rules": [
                rule("task_01", ("codex", "gpt-5.6-luna")),
                rule("task_02", CURSOR),
            ],
        },
        {
            "attempt": 2,
            "operation_id": "sha256:" + "3" * 64,
            "request_digest": "sha256:" + "4" * 64,
            "state": "terminal",
            "run_id": "run_attempt_2",
            "child_run_ids": ["implement_2", "review_2"],
            "failed_task_ids": [],
            "runtime_rules": [rule("task_02", CODEX)],
        },
    ]
    applied = [
        {
            "attempt": attempt["attempt"],
            "run_id": attempt["run_id"],
            "task_id": item["match"]["id"],
            "provider": item["runtime"]["provider"],
            "model": item["runtime"]["model"],
        }
        for attempt in attempts
        for item in attempt["runtime_rules"]
    ]
    return {
        "delivery_id": DELIVERY_ID,
        "journal": {
            "schema_version": 2,
            "deliveries": {
                DELIVERY_ID: {
                    "delivery_id": DELIVERY_ID,
                    "routing_generation_digest": GENERATION,
                    "attempt_ceiling": 4,
                    "token_ceiling": 1_000_000,
                    "state": "done",
                    "workspace_id": "workspace_demo",
                    "worktree_id": "worktree_demo",
                    "worktree_root": "/workspace/demo",
                    "task_set_digest": "c" * 64,
                    "attempts": attempts,
                }
            },
        },
        "runtime_applied": applied,
        "stored_config_calls": 0,
        "external_start_calls": 2,
        "merge_attempts": 0,
        "publication": {"verified": True, "head_sha": "d" * 40},
        "remote_head_sha": "d" * 40,
    }


class AssertDomainRoutingTest(unittest.TestCase):
    def test_accepts_fresh_run_fallback_with_exact_runtime_and_remote_evidence(self) -> None:
        assert_domain_routing(valid_evidence(), GENERATION, "task_02", CURSOR, CODEX)

    def test_rejects_same_run_or_replayed_primary_runtime(self) -> None:
        mutations = (
            lambda value: value["journal"]["deliveries"][DELIVERY_ID]["attempts"][
                1
            ].update(run_id="run_attempt_1"),
            lambda value: value["journal"]["deliveries"][DELIVERY_ID]["attempts"][1][
                "runtime_rules"
            ][0]["runtime"].update(provider="cursor", model=CURSOR[1]),
        )
        for mutate in mutations:
            with self.subTest(mutate=mutate):
                evidence = copy.deepcopy(valid_evidence())
                mutate(evidence)
                with self.assertRaises(AssertionError):
                    assert_domain_routing(evidence, GENERATION, "task_02", CURSOR, CODEX)

    def test_rejects_config_mutation_duplicate_children_or_unverified_remote(self) -> None:
        mutations = (
            lambda value: value.update(stored_config_calls=1),
            lambda value: value["journal"]["deliveries"][DELIVERY_ID]["attempts"][
                1
            ].update(child_run_ids=["implement_1"]),
            lambda value: value["publication"].update(verified=False),
        )
        for mutate in mutations:
            with self.subTest(mutate=mutate):
                evidence = copy.deepcopy(valid_evidence())
                mutate(evidence)
                with self.assertRaises(AssertionError):
                    assert_domain_routing(evidence, GENERATION, "task_02", CURSOR, CODEX)


if __name__ == "__main__":
    unittest.main()

"""Consumer-only validation of evidence emitted by the Go integration harness."""

import copy
import importlib.util
from pathlib import Path
import unittest


MODULE_PATH = Path(__file__).with_name("assert_parallel_delivery.py")
SPEC = importlib.util.spec_from_file_location("assert_parallel_delivery", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
validator = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(validator)


def emitted_evidence() -> dict:
    """A schema sample, not a constructed delivery lifecycle."""
    return {
        "identity": {
            "scenario_id": "parallel-demo",
            "delivery_id": "sha256:" + "a" * 64,
            "extension_version": "0.1.0-beta.6",
            "question_operation_id": "sha256:" + "b" * 64,
            "reviewed_head": "c" * 40,
            "retained_worktree_id": "wt_parallel_03",
            "retained_worktree_root": "/tmp/parallel-retained",
        },
        "initial_tasks": ["task_01", "task_02", "task_03", "task_04"], "initial_worktrees": 4,
        "child_starts": {"count": 4, "ids": ["run_started_task_01_1", "run_started_task_02_1", "run_started_task_03_1", "run_started_task_04_1"]},
        "frontend_route": {"provider": "cursor", "model": "grok-4.6"},
        "continuation": {"typed": True, "same_child": True, "same_worktree": True, "sibling_progress": True, "physical_execution": 2},
        "conflict": {"task_id": "task_02", "accepted_task_ids": ["task_01"], "retry_execution": 3},
        "integrated": ["task_01", "task_02", "task_03", "task_04", "task_05"],
        "commits": {"task_01": "feat(backend): fixture", "task_02": "fix(frontend): fixture", "task_03": "test: fixture", "task_04": "docs: fixture", "task_05": "feat(fullstack): fixture"},
        "dependent": {"task_id": "task_05", "admitted_after_prerequisites": True},
        "cleanup": {"retained": True, "blocker_code": "worktree_evidence_changed"},
        "replay": {"cleanup_journal_unchanged": True, "cleanup_removes_unchanged": True},
    }


def emitted_width_probe() -> dict:
    """A Go-harness width evidence schema sample, not lifecycle construction."""
    return {
        "width_probe": {
            "eligible_task_ids": ["task_01", "task_02", "task_03", "task_04", "task_05"],
            "started_child_ids": ["run_started_task_01_1", "run_started_task_02_1", "run_started_task_03_1", "run_started_task_04_1"],
            "started_child_count": 4,
            "pending_task_id": "task_05",
            "pending_task_attempts": 0,
            "prepare_replay_stable": True,
            "create_calls": 4,
        }
    }


class ParallelDeliveryEvidenceTests(unittest.TestCase):
    def test_accepts_go_harness_evidence_shape(self) -> None:
        validator.validate_harness_evidence(emitted_evidence())

    def test_rejects_mutable_cleanup_replay(self) -> None:
        evidence = copy.deepcopy(emitted_evidence())
        evidence["replay"]["cleanup_removes_unchanged"] = False
        with self.assertRaisesRegex(AssertionError, "replay"):
            validator.validate_harness_evidence(evidence)

    def test_accepts_go_harness_width_probe(self) -> None:
        validator.validate_harness_evidence(emitted_width_probe())


if __name__ == "__main__":
    unittest.main()

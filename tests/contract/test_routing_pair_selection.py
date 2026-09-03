import contextlib
import io
import unittest

from select_routing_pair import select_pair


class SelectRoutingPairTest(unittest.TestCase):
    def setUp(self):
        self.providers = [
            {"name": "usable", "auth_status": {"state": "unknown"}},
            {"name": "missing", "auth_status": {"state": "missing_cli"}},
        ]

    def test_prefers_live_available_over_unknown(self):
        models = [
            {
                "provider_id": "usable",
                "model_id": "a-unknown",
                "availability_state": "unknown",
            },
            {
                "provider_id": "usable",
                "model_id": "z-live",
                "available": True,
                "availability_state": "available_live",
            },
        ]

        self.assertEqual(select_pair(self.providers, models), ("usable", "z-live"))

    def test_falls_back_to_catalog_pair_when_nothing_is_live(self):
        models = [
            {
                "provider_id": "usable",
                "model_id": "fallback",
                "available": None,
                "availability_state": "unknown",
            }
        ]

        self.assertEqual(select_pair(self.providers, models), ("usable", "fallback"))

    def test_fallback_accepts_a_listed_but_unauthenticated_provider(self):
        models = [
            {
                "provider_id": "missing",
                "model_id": "cli-absent",
                "available": False,
                "availability_state": "unknown",
            }
        ]

        self.assertEqual(select_pair(self.providers, models), ("missing", "cli-absent"))

    def test_rejects_unlisted_provider_and_empty_catalog(self):
        models = [
            {"provider_id": "ghost", "model_id": "x", "availability_state": "unknown"},
            {"provider_id": "usable", "model_id": "hidden", "hidden": True},
        ]

        with self.assertRaisesRegex(RuntimeError, "no provider/model pair"):
            select_pair(self.providers, models)

    def test_excludes_explicitly_unavailable_and_missing_provider(self):
        models = [
            {
                "provider_id": "usable",
                "model_id": "unavailable",
                "available": False,
                "availability_state": "unavailable_live",
            },
            {
                "provider_id": "usable",
                "model_id": "stale-unavailable",
                "available": None,
                "availability_state": "unavailable_stale",
            },
            {
                "provider_id": "missing",
                "model_id": "live-but-provider-missing",
                "available": True,
                "availability_state": "available_live",
            },
        ]

        stderr = io.StringIO()
        with contextlib.redirect_stderr(stderr):
            pair = select_pair(self.providers, models)
        self.assertEqual(pair, ("missing", "live-but-provider-missing"))
        self.assertIn("WARN: no live provider/model pair", stderr.getvalue())

    def test_never_treats_ineligible_auth_states_as_live(self):
        models = []
        providers = []
        for index, state in enumerate(
            ("missing_cli", "missing_credential", "needs_login", "permission_denied")
        ):
            provider = f"provider-{index}"
            providers.append({"name": provider, "auth_status": {"state": state}})
            models.append(
                {
                    "provider_id": provider,
                    "model_id": "live-model",
                    "availability_state": "available_live",
                }
            )

        stderr = io.StringIO()
        with contextlib.redirect_stderr(stderr):
            pair = select_pair(providers, models)
        self.assertEqual(pair, ("provider-0", "live-model"))
        self.assertIn("WARN: no live provider/model pair", stderr.getvalue())

    def test_unknown_auth_remains_degraded_but_live(self):
        providers = [
            {"name": "unknown-provider", "auth_status": {"state": "unknown"}},
            {"name": "configured-provider", "auth_status": {"state": "configured"}},
        ]
        models = [
            {
                "provider_id": "unknown-provider",
                "model_id": "a-live",
                "availability_state": "available_live",
            },
            {
                "provider_id": "configured-provider",
                "model_id": "z-live",
                "availability_state": "available_live",
            },
        ]

        self.assertEqual(select_pair(providers, models), ("configured-provider", "z-live"))

    def test_null_auth_and_nameless_provider_are_bounded_unknown_evidence(self):
        providers = [
            {"name": "unknown-provider", "auth_status": None},
            {"auth_status": {"state": "configured"}},
        ]
        models = [
            {
                "provider_id": "unknown-provider",
                "model_id": "live-model",
                "availability_state": "available_live",
            }
        ]

        self.assertEqual(select_pair(providers, models), ("unknown-provider", "live-model"))


if __name__ == "__main__":
    unittest.main()

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

    def test_rejects_unknown_availability(self):
        models = [
            {
                "provider_id": "usable",
                "model_id": "fallback",
                "available": None,
                "availability_state": "unknown",
            }
        ]

        with self.assertRaisesRegex(RuntimeError, "no usable provider/model pair"):
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

        with self.assertRaisesRegex(RuntimeError, "no usable provider/model pair"):
            select_pair(self.providers, models)


if __name__ == "__main__":
    unittest.main()

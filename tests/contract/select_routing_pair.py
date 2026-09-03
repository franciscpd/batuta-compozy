#!/usr/bin/env python3
import json
import sys


def select_pair(providers, models):
    """Pick the lexicographically first live provider/model pair.

    A CI runner has no authenticated CLI, so no pair is live there. The dry-run
    only rejects an unknown provider (daemon `runtime_validation`
    `unknown_provider`); it accepts an unauthenticated provider and a model
    whose availability is unknown. When no live pair exists, fall back to the
    first catalog pair whose provider is listed at all and say so on stderr,
    so the routing-shape contract still runs while the live catalog is absent.
    """
    listed_providers = set()
    usable_providers = set()
    for row in providers:
        name = row.get("name")
        if not isinstance(name, str) or not name:
            continue
        listed_providers.add(name)
        state = (row.get("auth_status") or {}).get("state")
        if state not in {
            "missing_cli",
            "missing_credential",
            "needs_login",
            "permission_denied",
        }:
            usable_providers.add(name)

    live = []
    catalog = []
    for row in models:
        if row.get("hidden") or row.get("deprecated"):
            continue
        pair = (row["provider_id"], row["model_id"])
        if pair[0] not in listed_providers:
            continue
        catalog.append(pair)
        availability = row.get("availability_state")
        is_live = row.get("available") is True or availability in {"available", "available_live"}
        if pair[0] in usable_providers and is_live:
            live.append(pair)

    if live:
        return min(live)
    if catalog:
        provider, model = min(catalog)
        print(
            f"WARN: no live provider/model pair; using catalog pair {provider}/{model} "
            "for the routing-shape dry-run only",
            file=sys.stderr,
        )
        return provider, model
    raise RuntimeError("no provider/model pair in the catalogs")


def main():
    if len(sys.argv) != 3:
        raise SystemExit(f"usage: {sys.argv[0]} PROVIDERS_JSON MODELS_JSON")

    providers = json.load(open(sys.argv[1]))["providers"]
    models = json.load(open(sys.argv[2]))["models"]
    try:
        provider, model = select_pair(providers, models)
    except RuntimeError as error:
        raise SystemExit(f"prerequisite missing: {error}") from error
    print(provider)
    print(model)


if __name__ == "__main__":
    main()

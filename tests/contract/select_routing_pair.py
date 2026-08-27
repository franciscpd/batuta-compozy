#!/usr/bin/env python3
import json
import sys


def select_pair(providers, models):
    usable_providers = {
        row["name"]
        for row in providers
        if row.get("auth_status", {}).get("state")
        not in {"missing_cli", "missing_credential"}
    }

    candidates = []
    for row in models:
        availability = row.get("availability_state")
        live = row.get("available") is True or availability in {"available", "available_live"}
        if (
            row["provider_id"] not in usable_providers
            or row.get("hidden")
            or row.get("deprecated")
            or not live
        ):
            continue

        candidates.append((row["provider_id"], row["model_id"]))

    if not candidates:
        raise RuntimeError("no usable provider/model pair in live catalogs")

    provider, model = min(candidates)
    return provider, model


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

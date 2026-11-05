"""Rotate the assertion signing key:
`python -m cloud_api.scripts.rotate_assertion_key [--compromised]`

Adds a new signing key. The old key stays published for assertion.overlap_hours
so in-flight assertions and cached key sets keep working. With --compromised
the old keys are expired immediately (verifiers drop them at their next
refresh; assertions signed with them fail from now on).

Run it where assertion.keys_dir is mounted, with the same UNDERLEAF_CONFIG as
cloud_api. A running cloud_api notices the directory change on its next mint or
key set request; no restart is needed.
"""

import sys

import cloud_api
from cloud_api import load_config
from cloud_api.lib.assertion_lib import AssertionLib


def main() -> None:
    cfg = load_config()
    cloud_api.app_config = cfg
    a = cfg.assertion
    lib = AssertionLib(
        a.keys_dir,
        issuer=a.issuer,
        ttl_seconds=a.ttl_seconds,
        rotate_after_days=a.rotate_after_days,
        overlap_hours=a.overlap_hours,
    )
    key = lib.rotate(compromised="--compromised" in sys.argv[1:])
    print(f"new signing kid: {key.kid}")
    for k in lib.jwks()["keys"]:
        print(f"published: {k['kid']} not_after={k['not_after']}")


if __name__ == "__main__":
    main()

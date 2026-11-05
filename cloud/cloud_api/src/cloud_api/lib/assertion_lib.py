"""
assertion_lib.py -- issue short-lived, cluster-scoped assertions (RFD 5).

An assertion is the credential the cloud attaches when it forwards a remote
request (Cockpit / `ufctl connect`) down a cluster's tunnel. The cluster's
Orchestrator verifies it against the key set published by this module.

Keys
----
Assertions are signed with Ed25519 keys that are *dedicated* to this purpose:
they are generated here, stored apart from the root CA / OAuth JWT keypair
(auth_lib.py, cert_lib.py), and share no material with it. A token signed by
the CA/JWT key (RS256) is not an assertion, and an assertion does not
validate as an OAuth token.

The wire format and the verifier live in Go, shared/assertion (the Orchestrator
imports it); this module is the issuer. Both are checked against each other by
the vectors in shared/assertion/testdata/.

Storage
-------
One directory (config: assertion.keys_dir) holding, per key:

    <kid>.pem   private key, PKCS#8, mode 0600  (git-ignored by **/*.pem)
    <kid>.json  public metadata: kid, created_at, not_after (unix seconds)

The kid is the RFC 7638 thumbprint of the public key, so it is bound to the key
material and checked on load. No private key is ever written anywhere else.
Writes are atomic (temp file + rename). A single cloud_api process is assumed;
running several replicas needs the directory on shared storage or the keys
moved to a database (see the RFD-5 journal, step 7a open issues).

Rotation
--------
The signer always uses the newest key. A key is *published* from its creation
until `not_after`. When the current key reaches `rotate_after_days` a new one
is created (automatically, on first use after that moment, or explicitly with
rotate()), and every older key's `not_after` is capped at now + overlap so that
assertions already in flight, and verifiers that cache the key set, keep
working for the overlap window. `rotate(compromised=True)` expires old keys
immediately. Expired keys are deleted from disk.
"""

from __future__ import annotations

import base64
import hashlib
import json
import os
import pathlib
import secrets
import threading
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta

import jwt
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import (
    Ed25519PrivateKey,
    Ed25519PublicKey,
)

ALGORITHM = "EdDSA"
TOKEN_TYPE = "underleaf-assertion+jwt"
VALID_SCOPES = frozenset({"read", "deploy", "admin"})
MAX_TTL_SECONDS = 300


class InvalidAssertionRequestError(Exception):
    """Raised for invalid requests to mint an assertion."""


def _b64(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode()


def _raw_public(pub: Ed25519PublicKey) -> bytes:
    return pub.public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)


def thumbprint(pub: Ed25519PublicKey) -> str:
    """RFC 7638 thumbprint of an Ed25519 public key (used as the kid)."""
    canon = f'{{"crv":"Ed25519","kty":"OKP","x":"{_b64(_raw_public(pub))}"}}'
    return _b64(hashlib.sha256(canon.encode()).digest())


@dataclass
class AssertionKey:
    kid: str
    private_key: Ed25519PrivateKey
    created_at: datetime
    not_after: datetime

    def jwk(self) -> dict:
        return {
            "kty": "OKP",
            "crv": "Ed25519",
            "kid": self.kid,
            "alg": ALGORITHM,
            "use": "sig",
            "x": _b64(_raw_public(self.private_key.public_key())),
            "not_before": int(self.created_at.timestamp()),
            "not_after": int(self.not_after.timestamp()),
        }


class AssertionLib:
    def __init__(
        self,
        keys_dir: str,
        *,
        issuer: str = "underleaf-cloud",
        ttl_seconds: int = 60,
        rotate_after_days: int = 90,
        overlap_hours: int = 168,
        now: datetime | None = None,
    ) -> None:
        if not 0 < ttl_seconds <= MAX_TTL_SECONDS:
            raise ValueError(f"ttl_seconds must be in (0, {MAX_TTL_SECONDS}]")
        if rotate_after_days <= 0 or overlap_hours <= 0:
            raise ValueError("rotate_after_days and overlap_hours must be positive")
        self._dir = pathlib.Path(keys_dir)
        self.issuer = issuer
        self.ttl_seconds = ttl_seconds
        self._rotate_after = timedelta(days=rotate_after_days)
        self._overlap = timedelta(hours=overlap_hours)
        self._lock = threading.RLock()
        self._keys: list[AssertionKey] = []
        self._dir_stamp = 0
        now = now or datetime.now(UTC)
        with self._lock:
            self._load(now)
            self._ensure_current(now)

    # ------------------------------------------------------------------ API

    def mint(
        self,
        *,
        account_id: str,
        cluster_id: str,
        scopes: list[str] | tuple[str, ...],
        ttl_seconds: int | None = None,
        now: datetime | None = None,
    ) -> str:
        """Sign an assertion for `account_id` acting on `cluster_id`.

        This does NOT check that the account may act on the cluster; callers go
        through AssertionService.issue_for_cluster, which does.
        """
        if not account_id or not cluster_id:
            raise InvalidAssertionRequestError("account_id and cluster_id are required")
        scope_set = set(scopes)
        if not scope_set:
            raise InvalidAssertionRequestError("at least one scope is required")
        bad = scope_set - VALID_SCOPES
        if bad:
            raise InvalidAssertionRequestError(f"unknown scope(s): {sorted(bad)}")
        ttl = self.ttl_seconds if ttl_seconds is None else ttl_seconds
        if not 0 < ttl <= MAX_TTL_SECONDS:
            raise InvalidAssertionRequestError(
                f"ttl must be in (0, {MAX_TTL_SECONDS}] seconds"
            )

        now = now or datetime.now(UTC)
        with self._lock:
            self._sync(now)
            key = self._ensure_current(now)
        claims = {
            "iss": self.issuer,
            "sub": account_id,
            "aud": cluster_id,
            "scopes": sorted(scope_set),
            "iat": int(now.timestamp()),
            "exp": int(now.timestamp()) + ttl,
            "jti": secrets.token_urlsafe(16),
        }
        return jwt.encode(
            claims,
            key.private_key,
            algorithm=ALGORITHM,
            headers={"kid": key.kid, "typ": TOKEN_TYPE},
        )

    def jwks(self, now: datetime | None = None) -> dict:
        """The public key set: every key not yet past its not_after."""
        now = now or datetime.now(UTC)
        with self._lock:
            self._sync(now)
            self._ensure_current(now)
            return {
                "keys": [
                    k.jwk()
                    for k in sorted(self._keys, key=lambda k: k.created_at)
                    if k.not_after > now
                ]
            }

    def current_kid(self, now: datetime | None = None) -> str:
        now = now or datetime.now(UTC)
        with self._lock:
            self._sync(now)
            return self._ensure_current(now).kid

    def rotate(
        self, now: datetime | None = None, *, compromised: bool = False
    ) -> AssertionKey:
        """Create a new signing key and retire the others after the overlap
        (immediately if `compromised`). Returns the new key."""
        now = now or datetime.now(UTC)
        with self._lock:
            return self._rotate(now, compromised)

    # ------------------------------------------------------------ internals

    def _sync(self, now: datetime) -> None:
        """Re-read the directory if another process (the rotate script) changed
        it, so a rotation or a compromise expiry takes effect without a
        restart."""
        if self._stamp() != self._dir_stamp:
            self._load(now)
        else:
            self._purge(now)

    def _stamp(self) -> int:
        try:
            return self._dir.stat().st_mtime_ns
        except OSError:
            return 0

    def _current(self, now: datetime) -> AssertionKey | None:
        live = [k for k in self._keys if k.not_after > now]
        return max(live, key=lambda k: k.created_at, default=None)

    def _ensure_current(self, now: datetime) -> AssertionKey:
        cur = self._current(now)
        if cur is None or now >= cur.created_at + self._rotate_after:
            return self._rotate(now, compromised=False)
        return cur

    def _rotate(self, now: datetime, compromised: bool) -> AssertionKey:
        priv = Ed25519PrivateKey.generate()
        key = AssertionKey(
            kid=thumbprint(priv.public_key()),
            private_key=priv,
            created_at=now,
            not_after=now + self._rotate_after + self._overlap,
        )
        # Write the new key before touching the old ones: a crash in between
        # leaves two valid keys, never zero.
        self._write(key)
        cutoff = now if compromised else now + self._overlap
        for old in self._keys:
            if old.not_after > cutoff:
                old.not_after = cutoff
                self._write_meta(old)
        self._keys.append(key)
        self._purge(now)
        self._dir_stamp = self._stamp()
        return key

    def _purge(self, now: datetime) -> None:
        """Forget and delete keys that are past their not_after."""
        keep = []
        before = len(self._keys)
        for k in self._keys:
            if k.not_after > now:
                keep.append(k)
                continue
            for suffix in (".pem", ".json"):
                (self._dir / f"{k.kid}{suffix}").unlink(missing_ok=True)
        self._keys = keep
        if len(keep) != before:
            self._dir_stamp = self._stamp()

    def _load(self, now: datetime) -> None:
        self._keys = []
        self._dir.mkdir(parents=True, exist_ok=True)
        try:
            os.chmod(self._dir, 0o700)
        except OSError:
            pass
        for pem in sorted(self._dir.glob("*.pem")):
            meta_path = pem.with_suffix(".json")
            priv = serialization.load_pem_private_key(pem.read_bytes(), password=None)
            if not isinstance(priv, Ed25519PrivateKey):
                raise TypeError(f"{pem}: not an Ed25519 private key")
            kid = thumbprint(priv.public_key())
            if pem.stem != kid:
                raise RuntimeError(f"{pem}: file name does not match key thumbprint")
            meta = json.loads(meta_path.read_text())
            if meta.get("kid") != kid:
                raise RuntimeError(f"{meta_path}: kid mismatch")
            self._keys.append(
                AssertionKey(
                    kid=kid,
                    private_key=priv,
                    created_at=datetime.fromtimestamp(meta["created_at"], UTC),
                    not_after=datetime.fromtimestamp(meta["not_after"], UTC),
                )
            )
        self._purge(now)
        self._dir_stamp = self._stamp()

    def _write(self, key: AssertionKey) -> None:
        pem = key.private_key.private_bytes(
            encoding=serialization.Encoding.PEM,
            format=serialization.PrivateFormat.PKCS8,
            encryption_algorithm=serialization.NoEncryption(),
        )
        self._atomic_write(self._dir / f"{key.kid}.pem", pem)
        self._write_meta(key)

    def _write_meta(self, key: AssertionKey) -> None:
        meta = {
            "kid": key.kid,
            "created_at": int(key.created_at.timestamp()),
            "not_after": int(key.not_after.timestamp()),
        }
        self._atomic_write(self._dir / f"{key.kid}.json", json.dumps(meta).encode())

    @staticmethod
    def _atomic_write(path: pathlib.Path, data: bytes) -> None:
        tmp = path.with_name(path.name + ".tmp")
        fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "wb") as f:
            f.write(data)
            f.flush()
            os.fsync(f.fileno())
        os.replace(tmp, path)

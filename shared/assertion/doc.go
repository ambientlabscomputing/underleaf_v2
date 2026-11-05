// Package assertion defines the cloud-signed assertion used for remote access
// to a cluster's Orchestrator (RFD 5, "Remote access"), together with the
// primitives to verify one against a published key set.
//
// # Wire format
//
// An assertion is a compact JWS (RFC 7515), three base64url segments:
//
//	header  {"alg":"EdDSA","typ":"underleaf-assertion+jwt","kid":"<key id>"}
//	claims  {"iss","sub","aud","scopes","iat","exp","jti"}
//	signature over "<header>.<claims>" with Ed25519
//
// The algorithm is fixed (EdDSA over Ed25519). It is never read from the
// token to choose a verification method: a token whose header says anything
// else is rejected, which closes the classic "alg: none" and "HMAC with the
// public key as the secret" confusions. The dedicated "typ" additionally
// keeps other JWTs the cloud issues (OAuth access/refresh tokens, RS256) from
// ever being accepted here, and vice versa.
//
// # Key set
//
// The cloud publishes its public keys as a JWKS-style document (RFC 7517)
// with one OKP/Ed25519 key per entry, identified by "kid". Each entry also
// states "not_before" and "not_after" (unix seconds) so a verifier keeps a
// rotated-out key until exactly its stated expiry and no longer. The key set
// document is public; it carries no secrets.
//
// # Scope
//
// This package is deliberately dependency-light (standard library only) and
// stateless. Fetching, caching, refresh throttling, jti replay protection and
// the listeners that call Verify belong to the Orchestrator (RFD 5 step 7b).
package assertion

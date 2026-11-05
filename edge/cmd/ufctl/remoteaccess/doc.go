// Package remoteaccess implements `ufctl login`, `ufctl logout` and
// `ufctl connect <cluster>` (RFD 5, "ufctl connect").
//
// `ufctl connect` is an escape hatch for one operator on their own machine: it
// exposes http://localhost:<port> that proxies the cluster's Orchestrator UI and
// API (one origin) through the cloud. It is not a second Cockpit: no sharing,
// no listening beyond loopback, no features of its own.
//
// # Cloud contract
//
// Nothing below exists in cloud_api yet (RFD 5 step 7b is the edge half). This
// package defines the contract and is tested against a fake cloud. Paths are
// relative to the cloud API base URL (default
// https://underleafapp.com/api/v2/cloud).
//
// Login is the RFC 8628 device authorization grant, shaped like the cluster
// registration flow cloud_api already has:
//
//	POST /ufctl/device   {"device_name": "<hostname>"}
//	  -> {"device_code","user_code","verification_uri","verification_uri_complete","expires_in","interval"}
//	POST /ufctl/token    {"grant_type":"urn:ietf:params:oauth:grant-type:device_code","device_code":"..."}
//	  -> 200 {"device_key_id":"...","device_key":"..."}
//	  -> 400 {"error":"authorization_pending"|"slow_down"|"expired_token"|"access_denied"}
//
// The user approves the user code in the account UI while signed in. The
// device key is an opaque, revocable bearer secret bound to that account.
//
// Connect sends every request to
//
//	{base}/clusters/{cluster_id}/remote/{path}   Authorization: Bearer <device_key>
//
// and the cloud must: authenticate the device key to an account; check that the
// account OWNS the cluster; mint a fresh assertion for that request (account as
// subject, aud = the cluster id, scopes the owner is entitled to); strip any
// X-Underleaf-Assertion the client sent; open a stream to the cluster's agent
// (assertion.StreamPreamble) and forward the request with the assertion in
// the X-Underleaf-Assertion header; and relay the response, including WebSocket
// upgrades.
package remoteaccess

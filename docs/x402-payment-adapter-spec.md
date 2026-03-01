# x402 Payment Adapter Specification

This document defines the **x402 payment adapter** contract referenced from `VISION.md`. The adapter sits between a dir2mcp node and a third-party facilitator to enable optional HTTP 402 gating on selected MCP routes while keeping retrieval internals payment-agnostic.

## Purpose

Provide a clear, versioned contract so that:

* dir2mcp can emit and consume payment requirements without embedding blockchain logic.
* Facilitator providers can be swapped via configuration.
* Discovery and billing layers can interoperate with any compliant adapter.

## Normative baseline

The adapter MUST align with x402 v2 concepts and headers:

* `PAYMENT-REQUIRED` for payment challenges (`HTTP 402`)
* `PAYMENT-SIGNATURE` for client payment proof
* `PAYMENT-RESPONSE` for settlement/receipt metadata

Reference materials:

* x402 spec repository: <https://github.com/coinbase/x402/tree/main/specs>
* Facilitator API reference: <https://docs.cdp.coinbase.com/api-reference/v2/rest-api/x402-facilitator/x402-facilitator>
* Core concepts and migration notes:
	* <https://docs.cdp.coinbase.com/x402/core-concepts/how-it-works>
	* <https://docs.cdp.coinbase.com/x402/core-concepts/http-402>
	* <https://docs.cdp.coinbase.com/x402/migration-guide>

## Adapter contract

The contract defines, at a minimum, the following elements:  

* **Facilitator operations** – call facilitator verify/settle endpoints (CDP reference: `POST /v2/x402/verify`, `POST /v2/x402/settle`) and map their responses into dir2mcp transport behavior.  
* **Authentication** – adapter-to-facilitator auth must be explicit (for example API key auth for hosted facilitator, mTLS or signed requests for self-managed deployments).  
* **Payment state model** – canonical states `required -> verified -> settled` with failure branches (`invalid`, `rejected`, `expired`, `failed`). dir2mcp does not persist custodial payment state; facilitator is source of truth for verify/settle outcomes.  
* **Error codes and retries** – standard HTTP handling (`402`, `4xx`, `5xx`), idempotent settle calls, bounded retry/backoff for transient failures, and explicit non-retryable classes for invalid signatures/requirements mismatch.  
* **Network normalization** – CAIP-2 network identifiers are required at adapter boundaries (for example `eip155:8453`, `eip155:84532`, Solana CAIP-2 IDs).  
* **Discovery passthrough (optional)** – if Bazaar is enabled, expose extension metadata to facilitator discovery ingestion rather than implementing a custom discovery protocol in dir2mcp.  

## Out of scope

This adapter does not define:

* wallet key management,
* custodial fund handling,
* marketplace ranking/reputation logic,
* non-x402 billing schemes.

---

When protocol behavior changes, update this file and `SPEC.md` together.

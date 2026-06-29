# AGENTS.md

Source of truth for AI agents (Gemini CLI, Claude Code, Codex, etc.) working in this repo. Subagent guides may extend it; they may not override it.

## Project Overview

mdm-server is a minimal **Apple MDM** (Mobile Device Management) server for FlareX — the **Apple management plane**: enrollment lifecycle, the two device channels (check-in + command), APNs wake-up, and enrollment-profile (`.mobileconfig`) assembly.

**Device identity / PKI is deliberately out of scope** and delegated to two external services: `flarexio/identity` issues and verifies one-time SCEP challenges, and **Step-CA** signs the device certificates. mdm-server only asks identity for a challenge when it assembles an enrollment profile. NanoMDM is the reference for a production-grade core; each module here mirrors its corresponding design.

Built in the FlareX style: **Go-Kit + Clean Architecture + DDD / event sourcing**.

### Feature domains

- **Core service** (`./`, package `mdm`): `Service` drives both device channels (check-in → enrollment lifecycle transitions; command → the command/report loop + APNs wake). `Enroller` (`enroll.go`) fetches a one-time SCEP challenge from identity and assembles the `.mobileconfig`.
- **Enrollment aggregate** (`enrollment/`): the device's membership state machine (`Pending → Enrolled → Removed`), `Repository` port, and domain events.
- **Protocol types** (`checkin/`, `command/`, `profile/`): check-in messages (two-pass discriminated-union decode), the open-ended command/result envelope + `Queue` port, and Configuration Profile generation (SCEP + MDM + trust-anchor payloads).
- **Edge clients** (`push/`, `identity/`, `auth/`): certificate-based APNs `Pusher`, the mTLS client to identity's challenge endpoint, and JWKS-backed bearer-token verification.
- **Adapters** (`transport/http/`, `persistence/*`, `conf/`): the mTLS device server + admin HTTP, BadgerDB / in-memory repositories, and config loading.

### CLI surface (`cmd/mdm-server/`)

- `mdm-server [--path <dir>] [--port 8080] [--mtls-enabled] [--mtls-port 8443]` — run the server. `--path` (env `MDM_PATH`, default `~/.flarex/mdm`) is the working directory for `config.yaml`, `permissions.json`, and `certs/`. The admin/integration HTTP server always runs; the device-facing mTLS server only with `--mtls-enabled`.
- `mdm-server version [--all]` — print version (and BuildTime / GitCommit with `--all`).

## Architecture

Clean Architecture / Ports & Adapters, organized by feature and adapter boundary. **transport and infrastructure are both the outermost ring**: transport is inbound (driving), infra is outbound (driven); both depend on inner-domain interfaces, and the domain depends on neither.

Dependencies point inward: `transport/http` ──▶ `service.go` / domain ◀── `persistence`, `push`, `identity`. The domain defines the ports — `enrollment.Repository`, `command.Queue`, `push.Pusher`, `identity.Challenger` — and `cmd/mdm-server` is the composition root that binds each to a concrete adapter at the outermost layer.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full architecture decisions and real-device debugging notes, and [README.md](README.md) for how to run it.

## Critical Rules

- **Identity comes from the certificate, never the body.** Every `Service` method takes the authenticated `enrollment.ID` (resolved from the verified mTLS client-cert CN) separately from the message; the message supplies data only. Check-in/command body fields (UDID, EnrollmentID, …) are device-*claimed* and must not be trusted to identify the device.
- **PKI stays external.** mdm-server never signs a certificate or stores a private key for a device. It only requests a one-time SCEP challenge from identity and embeds it in the SCEP payload; Step-CA signs.
- **The domain depends on no adapter.** `service.go` / `enrollment` / `command` / `checkin` import no transport, persistence, push, or HTTP code. New backends (Redis queue, shared enrollment store, reverse-proxy header identity) plug in behind the existing ports without touching the domain.
- **Parse the envelope, keep the raw.** The MDM command set is open-ended and ever-growing: decode only the common envelope (`CommandUUID` + nested `RequestType`) and preserve the original plist in `Raw`. Do not try to enumerate every command. Check-in is the deliberate exception — its small closed set each needs distinct handling, so it is enumerated.
- **Admin endpoints are two-stage: authn then authz.** Authentication is an identity-signed EdDSA bearer token verified against identity's public JWKS; authorization is OPA (`core/policy`) matching each route's `domain.action` against `permissions.json`. Never collapse the two or bypass either.

## Design Decisions

- **Two bounded contexts, hard boundary.** `flarexio/identity` + Step-CA own identity/PKI (protocol-agnostic); `mdm` owns the Apple management plane (enrollment lifecycle, two channels, APNs, profile). The only coupling is mdm requesting a challenge at profile-assembly time.
- **Strict enrollment state machine.** `(none) --Authenticate--> Pending --TokenUpdate--> Enrolled --CheckOut--> Removed`. The watershed is `TokenUpdate`: only once the server holds the device's APNs push credentials can it ever initiate contact — `Enrollment.CanPush()` encodes that invariant (Enrolled + PushMagic + Token). `TokenUpdate` requires a prior `Authenticate`; a production server may instead upsert for resilience.
- **CheckOut is best-effort; reconcile from APNs.** A device may be wiped/destroyed/offline and never send `CheckOut`, so its absence is never proof a device is still enrolled. A push that reports the token dead (APNs `Unregistered`/410) reconciles the enrollment to `Removed`, recovering the state a missing `CheckOut` would have left stale. `CheckOut` is a soft delete (record kept as `Removed`) so a departed device stays queryable.
- **The command channel is one asynchronous report-and-fetch loop.** A single `PUT /server` turn both records the device's result for the previous command and returns the next one; an empty queue is a bare `200`. The queue is FIFO, keyed by a plain enrollment-ID string (decoupled from the aggregate), and a command is removed only on a terminal result. `Enqueue` is idempotent per `CommandUUID` (`ErrCommandExists`).
- **`NotNow` is kept, skipped this connection, retried next poll.** When a device defers a command, `Service.Command` passes `skipNotNow` so the server does not re-offer it in a tight loop within the same connection; on the next poll it is retried.
- **Enqueue wakes, tolerantly.** `Enqueue` queues then sends an APNs push so the device connects and pulls. Wake is best-effort: an unknown or not-yet-pushable device is skipped without error (the command waits for the device's next poll). The push payload carries only `PushMagic`.
- **Event sourcing is modeled but not yet wired to a bus.** The enrollment aggregate records domain events (`EnrollmentAuthenticated/TokenUpdated/CheckedOut`) via `core/events`, but persistence is currently synchronous (`Repository.Store`). The full flarexio flow — `Notify()` to a NATS bus, an `EventHandler` doing the store — is deferred to a later module. Keep events accurate now so that wiring is a drop-in.
- **The subject is taken from the token, not the URL.** `POST /enroll` reads the subject from the caller's `claims.sub`, so a user can only enroll themselves. The subject is both the SCEP challenge binding and the certificate CN.
- **Roles: self-service vs operator.** `/enroll` (`mdm::enroll.issue`) needs the **user** role (self-enrollment). `/enqueue` and `/enrollments` (`mdm::commands.*` / `mdm::enrollments.*`) need the **admin** role, granted by identity's `jwt.admins` allowlist. The bearer token's `aud` must include `auth.audience` (`mdm.flarex.io`).
- **Device-channel TLS cannot cross a TLS-terminating proxy.** mTLS needs the client cert to reach mdm, so the device endpoints (`/checkin`, `/server`) require **L4/TCP passthrough** (Traefik `HostSNI` + `passthrough: true`), with admin endpoints on a separate host via normal TLS — both on 443, split by SNI. The identity source is read from `r.TLS.PeerCertificates` and kept behind an `IdentityFunc` seam so a reverse-proxy-header mode can replace it without touching handlers.
- **Trust anchor travels in the profile.** The device-facing server certificate need not be publicly trusted: the enrollment `.mobileconfig` embeds the FlareX root (`certs/ca.crt`) as a Certificate payload, installed first, so the device trusts the private MDM/SCEP TLS certs it then contacts. The anchor is optional (skipped with a warning if no self-signed root is found).
- **Durability split: enrollment durable, queue ephemeral.** Enrollments persist to **BadgerDB** (`persistence/badger`, `<path>/db`) and survive a restart; the command queue stays in-memory (a lost command is simply re-enqueued). Horizontal scale swaps in a shared backend behind the unchanged `Repository` / `Queue` ports.
- **Certificate-purpose isolation against the VPN.** Step-CA's MDM provisioner template forces `OU=MDM`, drops `serverAuth`, and takes the CN from the CSR, so a device cannot change its own subject; OpenVPN (`OU=VPN`) accepts only `OU=VPN`, so an MDM cert cannot be used for VPN and vice versa.

## Code Style

- **Few comments; godoc only.** Every exported symbol gets one concise godoc line — enough for an LLM or `go doc` reader to know what it is without seeing the body. Omit the doc entirely when the name and signature already say it.
- **No essays in source.** Multi-paragraph rationale belongs in `docs/`, in a PR description, or in a commit message — not above a function. Inline comments are for non-obvious "why" only, never to restate what the next line does. (The protocol packages — `checkin`, `command`, `profile` — carry slightly heavier package/doc comments because the Apple wire format is genuinely non-obvious; keep new code lighter than that, not heavier.)
- **Test names carry the description.** Don't write `// TestX does Y` above `func TestX`; the name is the doc. Keep only comments that explain non-obvious test mechanics.
- **No section dividers** (`// --- handlers ---`). Code organization shows itself.
- **Treat comment churn as code churn.** Comments that drift out of sync are worse than no comment. If you change a function's contract, update or delete its doc in the same change.

## Go Tooling

- **For external packages, prefer `go doc` to reading source.** `flarexio/core`, the Badger, OPA, plist, and APNs dependencies, and anything outside this repo: use `go doc <pkg>`, `go doc <pkg>.<Symbol>`, or `go doc -src <pkg>.<Symbol>` — much cheaper than scanning an unfamiliar tree with `find` + `Read`. For this repo's own source, read files directly.
- **Use the default `GOCACHE`.** Run `go test ./...`, `go build`, etc. without prefixing `GOCACHE=` — the default external cache works. Never place the cache inside this repo (no `GOCACHE=.gocache`, no `.gocache/`). Only override with an outside-the-repo path (e.g. `/tmp/go-build-mdm`) when the default cache is actually broken locally.
- **Tests are the contract.** `go test ./...` is the gate. The protocol decoders, the enrollment state machine, the queue's NotNow semantics, and the HTTP transport all have table tests — extend them when you change behavior. For end-to-end (no real device, using scepclient + curl), see [docs/e2e-testing.md](docs/e2e-testing.md).

## Release Workflow

If an AI agent performs a release, preserve the agent attribution in the commit metadata as a `Co-Author`. Some tools add this automatically; for tools that do not, the agent must add it explicitly instead of omitting it. The Docker image is `flarexio/mdm`, built/released via GitHub Actions (`.github/workflows/`).

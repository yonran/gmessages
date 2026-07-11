# IMPLEMENTATION_NOTES.md — modern PullMessages receive path

Scope: adds a **modern-API (messages.google.com/web) `Messaging/PullMessages`
receive path** alongside the existing legacy `Messaging/ReceiveMessages`
long-poll, gated behind a new opt-in flag. Send / ack / auth / crypto / session
code is reused unchanged. Based strictly on `docs/MODERN_API.md` and
`docs/re/03-libgm-baseline.md`. **Nothing here has been run against the live
service — compile + static checks only.**

## What was implemented (compiles, `go build ./pkg/libgm/...` clean, gofmt clean, `go vet` clean)

1. **Endpoint constants** — `pkg/libgm/util/paths.go`:
   - `PullMessagesURL` / `PullMessagesURLGoogle` (Messaging service, `clients6`
     variant for the cookie'd path) — the required receive endpoint.
   - Optional auxiliaries, declared but unused by the receive loop:
     `PrewarmReceiverURL(+Google)` (Messaging), `ListIdentitiesURL`
     (Registration service), `GetFiUserStandingURL` (new `MessagesMultiDevice`
     service base). All paths are HIGH confidence from MODERN_API.md §2.

2. **Opt-in flag** — `pkg/libgm/client.go`: new `Client.UseModernReceive bool`,
   **defaults to false**. Added `receiveLoop()` which dispatches to the modern
   or legacy path; `Connect()` and `ConnectBackground()` now call `receiveLoop`
   instead of `doLongPoll` directly, so behavior is byte-identical to before
   unless a caller sets the flag.

3. **Parallel receive function** — `pkg/libgm/longpoll.go`: the body of the old
   `doLongPoll` was extracted into a shared `pollReceive(endpoint, …)`. Two thin
   wrappers now exist:
   - `doLongPoll` → `receiveEndpointLegacy` (unchanged behavior).
   - `doPullMessages` → `receiveEndpointModern` (the new path).
   A `receiveEndpoint` struct captures the only two documented differences: the
   request-body builder and the endpoint URL pair. The streaming response is
   parsed by the **same `readLongPoll`** dispatch (`LongPollingPayload` framing),
   reused verbatim.

4. **Reused unchanged** (no new crypto / builder / ack / pinger / fan-out):
   `AuthData`, `RequestCrypto`, `refreshAuthToken`, `buildMessage`/`sendMessage*`,
   `queueMessageAck`/`sendAckRequest`, `dittoPinger`, `HandleRPCMsg` /
   `decryptInternalMessage` / `handleUpdatesEvent`, and the `SignInGaia` /
   token-refresh machinery.

## What is stubbed / TODO (deliberately NOT fabricated)

- **`PullMessagesRequest` proto is NOT a generated message.** `protoc` is not
  available in this environment, and — more importantly — the only
  HIGH-confidence field is `header = 1`; every field below it (the resume
  cursor / ack-state) is UNKNOWN (schema-less JS decode + encrypted capture).
  Rather than invent field numbers, `doPullMessages` **clones
  `ReceiveMessagesRequest`** (auth/header blob already at field 1, matching the
  one proven fact) and POSTs it to `PullMessagesURL`. This is exactly the
  MODERN_API.md §4.2 step-2 recommendation. A documentation-only, non-generated
  `PullMessagesRequest` sketch is recorded as a comment in
  `gmproto/client.proto` (marked `NOT YET DEFINED … do not guess`).
- **No new `ActionType` / response-proto entries** for `GetFiUserStanding` /
  `ListIdentities`. Whether these ride the encrypted `OutgoingRPCMessage`
  mechanism (needing action enums) or are plain top-level RPCs (needing their
  own req/resp protos) is TBD without a decrypted capture. Only their URL
  constants are declared.
- **`PrewarmReceiver` / `Echo` / `ResponseHeader` / `StreamHeader` bodies** — not
  modeled (wrappers only, no getters, no prior art).
- **No modern framing fork.** `readLongPoll` (the `[[ … ]]` pblite chunk parser)
  is reused as-is on the assumption the modern stream is byte-identical; this is
  only MED-to-LOW confidence.

## What MUST be validated against the live service before this can actually work

Ranked by how likely it is to block a working receive:

1. **PullMessagesRequest body layout (#1 blocker).** The cloned
   `ReceiveMessagesRequest` may be rejected or may fail to advance, because:
   - the modern request almost certainly carries a **resume cursor / ack-state**
     field (field number UNKNOWN) the server needs to continue a batch;
   - `header = 1` is a `RequestHeader` in the modern schema, whose internal
     layout (does it embed the `AuthMessage`, or carry the tachyon token
     differently?) is UNKNOWN. If the server does not find the tachyon token
     where the cloned `AuthMessage` puts it, the call 401s.
   Validate: capture a real `PullMessages` request, diff its decoded bytes
   against the cloned `ReceiveMessagesRequest`, then define the true proto.

2. **Transport framing / mode.** `PullMessages` is declared **unary** by both the
   JS bundle and the GMSCore decompile, yet behaves like a minutes-long
   hanging poll. Confirm the response is the same chunked `[[ … ]]` pblite array
   `readLongPoll` expects; if it is a different framing, `readLongPoll` must be
   forked. Also confirm the client is expected to **re-arm a fresh PullMessages
   with an advanced cursor** on completion (the current loop just re-issues the
   same request, which is fine only if there is no cursor to advance).

3. **PullMessagesResponse body.** The repeated pulled-message / encrypted-blob
   field number(s) are UNKNOWN. We assume each delivered frame decodes as the
   libgm `IncomingRPCMessage` / `RPCMessageData` envelope; confirm this holds
   (enums `BugleRoute` / `MessageType` / `ActionType` numbering too) or decoding
   silently drops messages.

4. **Auth acceptance.** Confirm the existing SignInGaia + RegisterRefresh tokens
   are accepted by `PullMessages` unchanged (baseline assumes yes; revisit if it
   rejects the token).

5. **Cold-start prerequisites.** Whether `ListIdentities` and/or
   `GetFiUserStanding` (and `PrewarmReceiver`) must be called before
   `PullMessages` will deliver — sequence is INFERRED, not proven.

6. **Notification routing.** Orthogonal to receiving, but the whole point of the
   modern path: which registration field (`BrowserDetails.deviceType` WEB vs
   TABLET, Tachyon `capsArray`, or the runtime foreground `SendMessage`
   presence assertion) actually controls whether the phone keeps notifying is
   unproven (all registration payloads were encrypted).

## Single most important unknown

**The `PullMessagesRequest` body below `header = 1` — specifically the resume /
ack-state cursor field number and how the tachyon auth token is carried in the
modern `RequestHeader`.** Until a live capture pins this, the request is a
best-effort `ReceiveMessagesRequest` clone and may be rejected or fail to
advance the receive cursor.

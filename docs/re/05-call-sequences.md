# 05 — RPC call SEQUENCES the modern Google Messages web client makes

Goal: for each ACTION / SITUATION, give the ORDERED list of RPCs (direction +
purpose) the modern `messages.google.com/web` client issues, so `libgm` can
replicate the modern (`PullMessages`) receive path.

## Sources & how to read the confidence flags

Built from the recon docs in this folder:
- `01-js-bundle.md` — the closure-compiled JS bundle wire schema (RPC catalog,
  unary-vs-streaming, req/resp types).
- `02-prior-art.md` — GMSCore decompile + libgm + Tachyon ancestor protos.
- `04-auth-registration.md` — auth/registration/encryption path.

Confidence flags on every step:
- **[OBSERVED]** — an RPC method NAME was captured live this session on the wire
  (payloads were encrypted/blocked, so only the name + open/close timing is real
  evidence). Captured names: `SignInGaia`, `GetFiUserStanding`, `ListIdentities`,
  `SendMessage`, `PullMessages`, `AckMessages`, plus `…/v1.Pairing/…`.
- **[BUNDLE]** — the RPC exists in the JS bundle's descriptor table (`01`), so the
  client *can* call it, and its unary/streaming mode + req/resp types are known,
  but its position/timing in a sequence is not directly observed.
- **[SOURCE]** — behavior read from libgm/GMSCore/Tachyon source; used to infer
  what the encrypted body of a modern RPC most likely carries. libgm field
  numbers are NOT the modern client's — treat as family-level guidance only.
- **[INFERRED]** — reasoning from the above; not proven.

Hosts (all POST, gRPC-web / `application/json+protobuf` PBLite-ish framing):
- `https://instantmessaging-pa.googleapis.com/$rpc/…` and the
  `…clients6.google.com` variant.
- Messaging service: `…/v1.Messaging/<Method>`.
- Registration service: `…/v1.Registration/<Method>` (libgm's home for
  `SignInGaia`; the live capture grouped `SignInGaia` loosely under `Messaging`,
  low-confidence on the prefix since only names were seen — see `04` §2.1).
- Pairing service: `…/v1.Pairing/<Method>`.

Auth carried on EVERY Messaging/Registration RPC once signed in (`04` §2.6)
**[SOURCE]**: Tachyon `tachyonAuthToken` inside the request BODY (not a header);
Google cookies + `Authorization: SAPISIDHASH …`; `x-goog-api-key`;
`x-user-agent: grpc-web-javascript/0.1`.

---

## Cross-cutting: unary vs "long-poll stream"

Key reconciliation from `01` §"PullMessages transport": the bundle declares
`PullMessages` as **unary**, and `ReceiveMessages` as the separate legacy
**server_streaming** method. The live capture saw `PullMessages` stay open for
minutes and rarely re-issue. Best model **[INFERRED, MED]**: modern
`PullMessages` is a **hanging/long-poll unary** call (HTTP response streams/
chunks for minutes, returns a batch, then the client immediately re-issues a
fresh `PullMessages`). So "the receive stream" below = a single in-flight
`PullMessages` request, re-armed on completion — NOT a gRPC server-stream.
`PrewarmReceiver` (`01`) exists to warm this path. libgm's `ReceiveMessages`
long-poll is the same fabric, different verb/era (`04` §3.2).

---

## (1) Cold start / sign-in / registration

Ordered, on first load of a signed-in Google session:

1. **→ `Registration/SignInGaia`** (unary) — **[OBSERVED name]/[SOURCE shape]**.
   Registers this browser as a device on the GAIA (`GDitto`) network. Body
   (per libgm `04` §2.2, family-level): `DeviceID = "messages-web-<sessionID
   hex>"`, `BrowserDetails{deviceType:WEB, real browserType, real OS}`, refresh
   public key. libgm does this as two calls (`signInGaiaInitial` then
   `signInGaiaGetToken`); the modern client likely does likewise **[INFERRED]**.
   Response yields the Tachyon auth token (`TokenData`) + `Device{userID,
   sourceID, network}` + `browserUUID`.
2. **→ `Messaging/GetFiUserStanding`** (unary) — **[OBSERVED]**. Google Fi
   account-standing check (no prior art; absent from libgm). Gates Fi-specific
   UI; not required for receive. Order relative to ListIdentities not pinned.
3. **→ `Messaging/ListIdentities`** (unary) — **[OBSERVED]**. Enumerates the
   account's linked identities/devices (phone + other companions). Populates the
   device list; likely also how the client learns the primary device to talk to.
4. **→ `Registration/RegisterRefresh`** (unary) — **[BUNDLE]/[SOURCE]**, not in
   the observed name list this session but present in the schema; issued to mint/
   refresh the Tachyon token and (for the real client) register the browser
   Web-Push endpoint (`PushRegistration{url,p256dh,auth}`, `04` §2.5). May be
   folded into step 1's second call.
5. **→ `Messaging/PrewarmReceiver`** (unary) — **[BUNDLE]/[INFERRED]**. Warms the
   receive path before the first PullMessages. Optional; may be skipped.
6. Then proceed to (2) to open the receive path, and to (8) to load
   conversations.

Confidence: sequence backbone (SignInGaia → GetFiUserStanding → ListIdentities →
open receive) is **[OBSERVED]** for the four names; exact interleave of
RegisterRefresh/PrewarmReceiver and the SignInGaia two-step is **[INFERRED]**.

---

## (2) Opening the PullMessages receive stream; keep-alive / re-issue

1. **→ `Messaging/PullMessages`** (unary, long-poll) — **[OBSERVED]**. Opened once
   after sign-in; request body carries the RequestHeader (auth token) and
   (inferred, from Tachyon `GTPInboxPull`) a cursor / ack-state so the server
   knows where to resume (`02` §3, `04` §3.2) **[SOURCE/INFERRED]**.
2. The HTTP response **stays open for minutes** streaming/chunking; the client
   reads batches off it as they arrive (see (3)). **[OBSERVED]**
3. On response completion (timeout/end-of-batch), the client **immediately
   re-issues a fresh `PullMessages`** with an advanced cursor. Re-issue is
   **rare** because each call is long-lived. **[OBSERVED timing]/[INFERRED
   mechanism]**
4. Keep-alive: at the fabric level libgm uses heartbeat frames inside the
   long-poll body and a periodic `NOTIFY_DITTO_ACTIVITY` SendMessage ping
   (`02` §1.5). The modern client very likely relies on HTTP chunk/heartbeat
   frames to hold the socket; whether it also sends periodic
   `NOTIFY_DITTO_ACTIVITY` was **not observed** this session. **[SOURCE/INFERRED]**

---

## (3) A message ARRIVES while the tab is BACKGROUNDED (hidden)

Trigger: server pushes a message batch down the open `PullMessages` response.

1. **← `Messaging/PullMessages`** response batch delivers the new message
   (encrypted body) on the already-open stream. No new outbound request needed to
   RECEIVE. **[OBSERVED — the stream is the delivery channel]**
2. **→ `Messaging/AckMessages`** (unary) — **[OBSERVED]**. Client acks receipt of
   the delivered batch so the server advances the cursor / stops redelivering.
   This is a transport/delivery ack, NOT a read receipt.
3. **→ `Messaging/SendMessage`** (unary) — **[OBSERVED, SOMETIMES]**. Occasionally
   accompanies the ack. Purpose unproven; plausibly a delivery/annotation
   notification, NOT a MESSAGE_READ (the message stays UNREAD). **[INFERRED]**
4. The client keeps the message **UNREAD** and **the phone STILL notifies**
   (no read-receipt / no foreground assertion sent while hidden). **[OBSERVED]**

Contrast: it does NOT send a foreground/active assertion, and does NOT mark read.
That absence is what (per the working hypothesis) leaves the phone notifying.

---

## (4) FOREGROUND transition (hidden → visible)

Trigger: user brings the tab to the foreground (`visibilitychange` → visible).

1. **→ `Messaging/SendMessage`** (unary) — **[OBSERVED]**. Exactly ONE. The
   active/foreground/"this is the active device" assertion. libgm's analogue is
   `SetActiveSession()` = an `ActionType_GET_UPDATES(16)` SendMessage (`02` §1.5,
   `04` §5). Body (family-level, **[SOURCE]**) likely carries the GET_UPDATES /
   active-session action + a fresh sessionID.
2. **→ `Messaging/AckMessages`** (unary) — **[OBSERVED]**. Acks anything pending /
   confirms sync at the moment of activation.

Order observed: SendMessage then AckMessages (both fire together on the
transition). **[OBSERVED]**. This pair is the tightest correlate of
notification-suppression behavior (`04` §5 item 5).

---

## (5) BACKGROUND transition (visible → hidden)

- **Client sends NOTHING.** No SendMessage, no AckMessages, no explicit
  "inactive"/"background" RPC. **[OBSERVED]**. The receive `PullMessages` stream
  stays open; the client simply stops asserting foreground.

---

## (6) SENDING a message (user composes + sends)

1. **→ `Messaging/SendMessage`** (unary) — **[OBSERVED]/[BUNDLE]**. Req type
   `SendMessageRequest` = `{ OutgoingMessage message=2, RequestHeader header=3 }`;
   `OutgoingMessage.payload=12` is the **encrypted envelope** (bytes) containing
   the recipient/conversation/text — the client never introspects it, so
   recipient/conv fields are opaque on the wire (`01` §SendMessage). Action inside
   the encrypted body = `SEND_MESSAGE(3)` per libgm (`02` §1.2) **[SOURCE]**.
2. **← `SendMessageResponse`** = `{ ResponseHeader header=1 }` — ack of send;
   message-id/timestamp fields not read by the client. **[BUNDLE]**
3. The sent message + its server echo arrive back down the open **`PullMessages`**
   stream (state reconciliation), which is then **AckMessages**'d as in (3).
   **[INFERRED]**

Note: there is NO separate "typing"/CreateConversation call required for a plain
send; `SmartMessaging/CreateConversation` exists in the bundle for new-thread
creation only (`01`). **[BUNDLE]**

---

## (7) ACKING

- **→ `Messaging/AckMessages`** (unary) — **[OBSERVED]**. Req type
  `AckMessagesRequest = { RequestHeader header=1, <repeated ack-entry — encrypted/
  opaque> }` (`01` §AckMessages). Purpose = transport-level delivery ack that
  advances the receive cursor so the server stops redelivering.
- Fires: after every received `PullMessages` batch (3); alongside the foreground
  assertion (4). **[OBSERVED]**
- A READ RECEIPT (marking a conversation read) is a DIFFERENT operation — it goes
  out as a **`SendMessage`** carrying `ActionType_MESSAGE_READ(10)` in the
  encrypted body (`02` §1.2), NOT via AckMessages. Not separately observed this
  session but that's the libgm-family mechanism. **[SOURCE/INFERRED]**

---

## (8) LISTING conversations / contacts / messages

1. **→ `Messaging/ListIdentities`** (unary) — **[OBSERVED]**. Device/identity
   enumeration at startup (see (1)); not the conversation list itself.
2. Conversation list, contact list, and message history are **NOT distinct
   observed RPC names**. In the libgm family they are fetched as
   **`SendMessage`** calls carrying list/GET actions in the encrypted body
   (e.g. `LIST_CONVERSATIONS`, `LIST_CONTACTS`, `LIST_MESSAGES`, `GET_UPDATES`
   under `ActionType`; `02` §1.2), with the results delivered back down the
   **`PullMessages`** stream and paged via further `SendMessage`s. So on the wire
   the modern client's "load conversations" ≈ one or more
   `SendMessage`+`PullMessages`(delivery)+`AckMessages` cycles, indistinguishable
   by method name from a normal send. **[SOURCE/INFERRED — the encrypted action
   bytes were not captured]**
3. Contact-decoration / rich previews: `SmartMessaging/GetContentDecoration`
   exists in the bundle for this. **[BUNDLE]**

Honest gap: because payloads were encrypted, the conversation/contact/message
list operations could NOT be individually resolved to method names beyond
`SendMessage`/`PullMessages`/`AckMessages`. **[OBSERVED limit]**

---

## (9) RECONNECT after drop / auth refresh

On receive-stream drop (network blip, server closes the long-poll early):

1. **→ `Messaging/PullMessages`** (unary) — **[OBSERVED behavior / INFERRED
   trigger]**. Re-armed with the last-known cursor to resume the receive path
   (same as (2) step 3, but triggered by error rather than normal completion).
   May be preceded by **`PrewarmReceiver`** **[BUNDLE]**.
2. If the Tachyon token is stale / a 401-class error occurs:
   **→ `Registration/RegisterRefresh`** (unary) — **[BUNDLE]/[SOURCE]** to mint a
   fresh `tachyonAuthToken` (signed with the refresh key, `04` §2.5), THEN retry
   `PullMessages`.
3. On harder failure (device/session invalidated): fall back to
   **→ `Registration/SignInGaia`** again to re-register, then repeat (1)/(2).
   **[INFERRED]**. The bundle also carries `Start/StopFallbackToTachyonPullMessages`
   counters, i.e. the client can fall back between `PullMessages` and the legacy
   `ReceiveMessages` receive transports (`01` §PullMessages transport). **[BUNDLE]**
4. On resuming foreground while reconnecting, the foreground pair (4) fires.

Confidence: the RECONNECT trigger and RegisterRefresh/SignInGaia escalation are
**[INFERRED]** from schema + standard token-refresh patterns; only the
`PullMessages` re-issue timing is **[OBSERVED]**.

---

## Quick reference — who fires what

| Situation | Ordered RPCs | Flag |
|---|---|---|
| Cold start | SignInGaia → GetFiUserStanding → ListIdentities → (RegisterRefresh, PrewarmReceiver) → PullMessages | OBSERVED core |
| Open receive | PullMessages (long-poll), re-issued on completion | OBSERVED |
| Msg arrives (hidden) | ←PullMessages batch → AckMessages (± SendMessage); stays UNREAD; phone notifies | OBSERVED |
| Foreground (hidden→visible) | SendMessage (active assertion) → AckMessages | OBSERVED |
| Background (visible→hidden) | (nothing) | OBSERVED |
| Send message | SendMessage(payload=12 encrypted) → resp; echo via PullMessages → AckMessages | OBSERVED/BUNDLE |
| Ack | AckMessages (delivery cursor); read-receipt = SendMessage MESSAGE_READ | OBSERVED / SOURCE |
| List convos/contacts/msgs | SendMessage(list action, encrypted) → PullMessages(delivery) → AckMessages | INFERRED |
| Reconnect / auth refresh | PullMessages re-arm; on 401 → RegisterRefresh → retry; hard fail → SignInGaia | OBSERVED / INFERRED |

## Biggest unknowns (do not fabricate)

- All encrypted bodies: which `ActionType` each `SendMessage` carries, the
  AckMessages ack-entry list, PullMessages cursor/batch fields, the
  list-conversations/contacts operations. Only method NAMES + open/close timing
  were observed.
- Whether a periodic `NOTIFY_DITTO_ACTIVITY` keep-alive is sent by the modern
  client (libgm sends one; not observed here).
- Whether the SendMessage that sometimes accompanies a hidden-arrival ack (3.3)
  is a distinct action from the foreground assertion (4.1).
- Service-prefix of `SignInGaia` (Registration per source vs Messaging per the
  loose live grouping).

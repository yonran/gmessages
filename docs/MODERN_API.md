# MODERN_API.md — instantmessaging v1 `Messaging` service (modern Google Messages web)

Authoritative, merged spec for the **modern** `messages.google.com/web` receive
path (`PullMessages`), reconstructed from static reverse-engineering + prior art.
It supersedes nothing in `libgm`; it is the target for adding a modern receive
path alongside the existing `ReceiveMessages` implementation.

## Evidence sources & confidence legend

Every field/claim below is tagged with a confidence level and an evidence source.

Confidence:
- **HIGH** — verbatim from a primary source (JS bundle descriptor, libgm proto,
  GMSCore decompile).
- **MED** — strong inference from a primary source (e.g. codec→type calibration,
  long-poll interpretation).
- **LOW** — speculation / analogy from a different generation or fabric.
- **UNKNOWN** — genuinely not recoverable from static analysis; needs live capture.

Evidence source codes:
- **[JS]** — `01-js-bundle.md`: the closure-compiled `messagesweb.mw` JS bundle
  (`/tmp/gm_mw_b.js`, 1,325,778 bytes, public, no auth). Verbatim RPC descriptors
  and getter/setter call sites.
- **[PRIOR]** — `02-prior-art.md`: GMSCore decompile (`Romern/gms_decompiled`),
  Tachyon `GTPInbox*` ancestor protos, Chromium Nearby Express protos, ecosystem
  ports.
- **[LIBGM]** — `03-libgm-baseline.md` + libgm source in this repo
  (`pkg/libgm/...`): the existing `ReceiveMessages`-era implementation.
- **[AUTH]** — `04-auth-registration.md`: auth/registration/encryption path.
- **[SEQ]** — `05-call-sequences.md`: observed per-action RPC ordering.
- **[OBSERVED]** — captured live on the wire this session. **Method NAMES + open/
  close timing ONLY; all payloads were encrypted/blocked.**

> **Load-bearing caveat, stated once and true everywhere below:** the modern
> client is *schema-less on decode* [JS] and every payload body was *encrypted*
> when observed [OBSERVED]. The only field numbers/types that are HIGH are the
> thin request/response *wrappers* the JS explicitly touches. libgm field numbers
> are from the `ReceiveMessages` era and are the **same protocol family, not the
> same wire schema** — never treat a libgm field number as proven for the modern
> client [AUTH §6].

---

# 1. Protobuf definitions (proto3) — modern v1 `Messaging`

Package `google.internal.communications.instantmessaging.v1`.
JSPB messages extend `_.n`. Type-identifier column names the obfuscated JS class.

## 1.1 PullMessages — FIRST and most complete

### Wrapper messages (recovered)

```proto
// tTa [JS] — PullMessagesRequest. Only field 1 is touched by client code.
message PullMessagesRequest {
  RequestHeader header = 1;   // _.T(this,_.fz,1)   HIGH [JS]
  // --- everything below is UNKNOWN from static analysis; encrypted/untouched ---
  // Inferred (LOW, from Tachyon GTPInboxPullRequest [PRIOR §3] / [AUTH §3.2]):
  //   a resume CURSOR and/or ack-state so the server knows where to continue.
  //   In GTPInboxPullRequest the cursor is `int64 startTimestamp = 1` with the
  //   header at field 2 — but the modern wrapper puts the header at field 1, so
  //   the ancestor's field numbers do NOT transfer. Cursor field number UNKNOWN.
}

// k6a [JS] — PullMessagesResponse. Deserialized per-batch via _.wd(k6a)=_.yda(k6a,b).
message PullMessagesResponse {
  ResponseHeader header = 1;  // _.T(this,_.Fz,1)   HIGH [JS]
  // --- UNKNOWN: the repeated pulled-message / encrypted-blob field(s). ---
  // The client NEVER reads them by a named getter (schema-less decode), so no
  // field number is recoverable statically. Inferred shape (LOW, from
  // GTPInboxPullResponse `repeated GTPInboxMessage messagesArray = 1` [PRIOR §3]):
  //   repeated <IncomingMessage> messages = ?;  // each an encrypted envelope
}
```

### How the body is ACTUALLY consumed (the reusable model) [LIBGM]

The JS never names the batch fields, but the libgm-family fabric decodes the
receive stream as a sequence of `LongPollingPayload` frames, and the modern
`PullMessages` response is **believed identical on the wire** until a capture
disproves it [LIBGM (e)]. This is the concrete structure an implementation should
start from (all field numbers HIGH *for the `ReceiveMessages` era* [LIBGM], but
only MED-to-LOW that they are byte-identical for `PullMessages`):

```proto
// rpc.proto (libgm) — the streaming frame the receive body chunks into. [LIBGM]
message LongPollingPayload {
  IncomingRPCMessage data      = 2;   // a delivered message
  util.EmptyArr      heartbeat = 3;   // keep-alive tick
  StartAckMessage    ack       = 4;   // { int32 count = 1; } startup dedup skip
  util.EmptyArr      startRead = 5;
}
message IncomingRPCMessage {          // libgm HIGH; modern MED
  string responseID          = 1;
  BugleRoute bugleRoute      = 2;
  uint64 startExecute        = 3;
  MessageType messageType    = 5;
  uint64 finishExecute       = 6;
  uint64 microsecondsTaken   = 7;
  authentication.Device mobile  = 8;
  authentication.Device browser = 9;
  bytes  messageData         = 12;  // RPCMessageData or RPCPairData (encrypted)
  string signatureID         = 17;
  string timestamp           = 21;
  GDittoSource gdittoSource   = 23;  // { int32 deviceID = 2; }
}
message RPCMessageData {              // messageData=12 decrypts into this [LIBGM]
  string sessionID          = 6;     // note: NOT contiguous; see rpc.proto
  ActionType action         = ?;     // selects the decrypted response proto
  bytes  unencryptedData    = 5;
  bytes  encryptedData      = 8;
  bytes  encryptedData2     = 11;
}
```

> **Transport reconciliation (a real contradiction — do NOT smooth over):**
> - [JS] declares `PullMessages` **`"unary"`** (verbatim descriptor `l6a`,
>   `mode="unary"`), dispatched via the generic unary caller `_.kE`. HIGH.
> - [PRIOR] GMSCore decompile also registers `PullMessages` as **`UNARY`**
>   (`chtu.UNARY`, `bcmr.java`). HIGH.
> - [OBSERVED]/[SEQ]/[AUTH] the live call **stays open for minutes** and rarely
>   re-issues — it *behaves* like a server stream.
> - Reconciliation (MED, inferred): modern `PullMessages` is a **hanging/long-poll
>   unary** call — the HTTP response chunks for minutes, returns one batch, then
>   the client immediately re-arms a fresh `PullMessages` with an advanced cursor.
>   NOT a gRPC server-stream. The legacy `ReceiveMessages` *is* the true
>   `server_streaming` method [JS descriptor `m6a`], present in the bundle as a
>   fallback (`Start/StopFallbackToTachyonPullMessages` counters exist [JS]).
> - **05-call-sequences and 03-libgm-baseline both at points call PullMessages
>   "server-streaming"; that phrasing is the observed BEHAVIOR, and it
>   CONTRADICTS the two HIGH static sources that say unary. Treat unary-long-poll
>   as the working model; the true framing (`[[ … ]]` pblite chunk array vs
>   something new) is UNKNOWN until captured [LIBGM (e) step 3].**

## 1.2 SendMessage

```proto
// _.QD [JS] — SendMessageRequest
message SendMessageRequest {
  OutgoingMessage message = 2;   // _.T(this,_.Ez,2)   HIGH [JS]
  RequestHeader   header  = 3;   // _.T(this,_.fz,3)   HIGH [JS]
}
// _.Ez [JS] — OutgoingMessage
message OutgoingMessage {
  bytes payload = 12;            // _.un(this,12) ByteString  HIGH [JS]
                                 // = the ENCRYPTED envelope (recipient / conv /
                                 // text / ActionType). Client never introspects.
  // UNKNOWN: recipient / conversation / type fields (opaque inside payload=12,
  // or set via generic non-getter paths).
}
// _.iD [JS] — SendMessageResponse
message SendMessageResponse {
  ResponseHeader header = 1;     // _.T(this,_.Fz,1)   HIGH [JS]
  // UNKNOWN: message-id / timestamp fields (present per GTPInboxSendResponse
  //   [PRIOR §3] but NOT read by the JS client, so field numbers unrecoverable).
}
```

Action carried inside the encrypted `payload=12` for a user send =
`ActionType.SEND_MESSAGE (3)` [LIBGM/PRIOR §1.2]. A read receipt is also a
`SendMessage`, carrying `ActionType.MESSAGE_READ (10)` — NOT an `AckMessages`
[SEQ §7]. UNKNOWN whether the modern encrypted body reuses these exact action
numbers.

## 1.3 AckMessages

```proto
// iTa [JS] — AckMessagesRequest
message AckMessagesRequest {
  RequestHeader header = 1;      // _.T(this,_.fz,1)   HIGH [JS]
  // UNKNOWN: repeated ack-entry / message-id list (encrypted/untouched by JS).
  // libgm-family shape (MED [LIBGM]): repeated Message { string requestID = 1;
  //   authentication.Device device = 2; } — advances the receive cursor so the
  //   server stops redelivering. This is a DELIVERY ack, not a read receipt.
}
// _.$3a [JS] — AckMessagesResponse
message AckMessagesResponse {
  ResponseHeader header = 1;     // _.T(this,_.Fz,1)   HIGH [JS]
}
```

## 1.4 ReceiveMessages (LEGACY server-streaming — for contrast / fallback)

```proto
// uTa [JS] — ReceiveMessagesRequest
message ReceiveMessagesRequest {
  RequestHeader header = 1;      // _.T(this,_.fz,1)   HIGH [JS]
  // libgm body (HIGH [LIBGM]): authentication.AuthMessage auth = 1;
  //   nested UnknownEmptyObject2{ UnknownEmptyObject1 unknown = 2 } at field 4.
  // NOTE the header-vs-auth discrepancy: JS models field 1 as a RequestHeader
  // (_.fz); libgm models field 1 as an AuthMessage. Likely the same bytes viewed
  // through two schemas (the auth token lives in the header family). MED.
}
// _.Jz [JS] — ReceiveMessagesResponse (server_streaming, descriptor m6a)
message ReceiveMessagesResponse {
  StreamHeader header = 1;       // _.T(this,vTa,1)   HIGH [JS] (distinct hdr type)
  // UNKNOWN: message body field(s).
}
```

## 1.5 PrewarmReceiver / Echo (warm + liveness; wrappers only)

```proto
message PrewarmReceiverRequest  { /* h6a [JS]; fields UNKNOWN */ }
message PrewarmReceiverResponse { /* i6a [JS]; fields UNKNOWN */ }
message EchoRequest             { /* kTa [JS]; fields UNKNOWN */ }
message EchoResponse            { /* f6a [JS]; fields UNKNOWN */ }
```

## 1.6 Cross-service messages the modern cold-start also calls

### Registration.SignInGaia

```proto
// u7a [JS] — SignInGaiaRequest (wrapper only from JS)
message SignInGaiaRequest {
  RequestHeader header = 1;      // _.T(this,_.fz,1)   HIGH [JS]
  // UNKNOWN from JS: device/app/GAIA-token fields.
  // libgm full body (HIGH [LIBGM/AUTH §2.2], modern MED — same family):
  //   AuthMessage authMessage = 1;   // requestID, network="GDitto", configVersion
  //   Inner inner = 2;               // DeviceID{unknownInt1=3, deviceID=
  //                                  //   "messages-web-<sessionID hex>"},
  //                                  //   Data someData=36 = PKIX refresh pubkey
  //   int32  unknownInt3 = 3;        // 1 on the initial call
  //   string network = 4;            // "GDitto"
}
// _.k4a [JS] — SignInGaiaResponse
message SignInGaiaResponse {
  ResponseHeader header = 1;     // _.T(this,_.Fz,1)   HIGH [JS]
  // field 2 maybeBrowserUUID (pblite_binary) — HIGH [LIBGM], not surfaced in JS
  RegistrationData field3 = 3;   // _.T(this,_.Hz,3)   HIGH [JS] (type _.Hz,
                                 //   body UNKNOWN; libgm calls this deviceData:
                                 //   DeviceWrapper{Device{userID,sourceID,network}})
  // field 4 tokenData (tachyonAuthToken + TTL) — HIGH [LIBGM], not in JS getters
}
message Hz { /* UNKNOWN from JS — no getters; = libgm DeviceData family */ }
```

> **Service-prefix contradiction (do NOT smooth over):**
> - [OBSERVED]/[SEQ] logged `SignInGaia` loosely grouped under `…/v1.Messaging/`.
> - [LIBGM]/[PRIOR]/[AUTH §2.1] every SOURCE places `SignInGaia` on
>   `…/v1.Registration/` (on the `clients6` host).
> - Resolution: only method NAMES were observable, so the live service-prefix
>   grouping is LOW confidence; **`Registration` is the correct home per every
>   primary source.** Implement against `Registration/SignInGaia`.

### Registration.ListIdentities

```proto
// _.k7a [JS] — ListIdentitiesRequest
message ListIdentitiesRequest { RequestHeader header = 1; }  // HIGH [JS]
// _.l7a [JS] — ListIdentitiesResponse
message ListIdentitiesResponse {
  ResponseHeader header = 1;   // HIGH [JS]
  // UNKNOWN: repeated Identity — not read by a named getter.
}
// _.cz [JS] — Identity (seen via SignInSecondary.getId)
message Identity {
  IdentityType type = 1;       // _.tn(this,1) enum   HIGH [JS]
  string       id   = 2;       // _.q(this,2)         HIGH [JS]
}
```

No prior art exists for `ListIdentities` on this service [PRIOR §2 headline #5].
Purpose (INFERRED [SEQ §1]): enumerate the account's linked identities/devices;
likely how the client learns the primary device to address.

### MessagesMultiDevice.GetFiUserStanding

```proto
// _.Jab [JS] — GetFiUserStandingRequest
message GetFiUserStandingRequest { RequestHeader header = 1; }  // HIGH [JS]
// _.dD [JS] — GetFiUserStandingResponse
message GetFiUserStandingResponse {
  ResponseHeader header = 1;   // HIGH [JS]
  // UNKNOWN: standing/status enum field.
}
```

**Zero prior art anywhere** [PRIOR §2 headline #4]. Google Fi standing check;
gates Fi-specific UI; NOT required for the receive path [SEQ §1]. Note the JS
places this on the **`MessagesMultiDevice`** service (`_.Jab`/`_.dD` descriptors
[JS]) even though [OBSERVED]/[SEQ] loosely grouped it under `Messaging` — same
names-only caveat as SignInGaia.

## 1.7 Shared header types

```proto
// _.fz [JS] — RequestHeader. Only field 1 has a named getter (Uf → _.q(this,1)).
message RequestHeader {
  string field1 = 1;   // _.q(this,1) — an id/token string   HIGH [JS]
  // Remaining fields via the _.Gz descriptor array (MED-LOW, format decode
  // unverified against the interpreter Wc()):
  //   <qr>  field2   = 2;    // numeric/double family (codec _.qr)
  //   repeated <Jr> field100 = 100;   // 2+98=100, negative => repeated
  // Codec↔type calibration (MED [JS]): _.Er↔bytes, _.qr↔double, _.Jr/_.eza↔string.
}
// _.Fz [JS] — ResponseHeader. NO named getters anywhere → fully opaque.
message ResponseHeader { /* UNKNOWN fields */ }
// vTa [JS] — streaming header, used ONLY by ReceiveMessagesResponse. No getters.
message StreamHeader { /* UNKNOWN */ }
```

> The RequestHeader carries the auth material. In the libgm family the auth token
> lives in `AuthMessage.tachyonAuthToken (bytes, field 6)` inside the request body
> [AUTH §2.6]; whether the modern `RequestHeader (_.fz)` embeds an `AuthMessage`
> submessage or carries the token differently is UNKNOWN (only `field1` string is
> proven).

## 1.8 Enums (from the libgm family — modern numbering UNVERIFIED)

None of these enum *values* were observed on the modern wire (encrypted). They
are HIGH for the `ReceiveMessages` era [LIBGM/PRIOR §1.2] and are the starting
model for decoding modern encrypted bodies.

```proto
enum BugleRoute  { Unknown = 0; GaiaEvent = 7; PairEvent = 14; DataEvent = 19; }
enum MessageType { UNKNOWN = 0; BUGLE_MESSAGE = 2; GAIA_1 = 3;
                   BUGLE_ANNOTATION = 16; GAIA_2 = 20; }
enum ActionType {  // 50+ values; the presence/activity-relevant subset:
  SEND_MESSAGE = 3;            MESSAGE_READ = 10;
  BROWSER_PRESENCE_CHECK = 11; GET_UPDATES = 16;   ACK_BROWSER_PRESENCE = 17;
  NOTIFY_DITTO_ACTIVITY = 22;  IS_BUGLE_DEFAULT = 31;  PREWARM = 48;
  // gaia-pairing set: 41,42,44,45,46,47; LINK_RCS_IDENTITY=50; UNLINK=51; ...
}
enum DeviceType { UNKNOWN_DEVICE_TYPE = 0; WEB = 1; TABLET = 2; PWA = 3; }
```

---

# 2. Endpoint constants

Host bases [LIBGM `util/paths.go` / AUTH §2.1, HIGH]:
```
instantMessagingBaseURL        = https://instantmessaging-pa.googleapis.com
instantMessagingBaseURLGoogle  = https://instantmessaging-pa.clients6.google.com
```
Path template: `<base>/$rpc/google.internal.communications.instantmessaging.v1.<Service>/<Method>`

| Service | Method | Mode | Req [JS] | Resp [JS] | In libgm today? |
|---|---|---|---|---|---|
| Messaging | **PullMessages** | unary (long-poll) [JS/PRIOR] | `tTa` | `k6a` | **NO — add** |
| Messaging | SendMessage | unary [JS] | `_.QD` | `_.iD` | yes (shared) |
| Messaging | AckMessages | unary [JS] | `iTa` | `_.$3a` | yes (shared) |
| Messaging | ReceiveMessages | server_streaming [JS] | `uTa` | `_.Jz` | yes (legacy path) |
| Messaging | PrewarmReceiver | unary [JS] | `h6a` | `i6a` | **NO** |
| Messaging | Echo | unary [JS] | `kTa` | `f6a` | no |
| Registration | SignInGaia | unary [JS/LIBGM] | `u7a` | `_.k4a` | yes (Registration svc) |
| Registration | RegisterRefresh | unary [LIBGM] | — | — | yes |
| Registration | ListIdentities | unary [JS] | `_.k7a` | `_.l7a` | **NO — add** |
| Registration | SignInSecondary | unary [JS] | `_.w7a` | `_.oE` | no |
| MessagesMultiDevice | GetFiUserStanding | unary [JS] | `_.Jab` | `_.dD` | **NO — optional** |
| Pairing | RegisterPhoneRelay / RefreshPhoneRelay / GetWebEncryptionKey / RevokeRelayPairing | unary [JS/LIBGM] | — | — | yes (QR path) |
| SmartMessaging | GetContentDecoration / CreateConversation | unary [JS] | — | — | no |

Constants to ADD [LIBGM (e)]:
```
PullMessagesURL       = instantMessagingBaseURL       + "/$rpc/…v1.Messaging/PullMessages"
PullMessagesURLGoogle = instantMessagingBaseURLGoogle + "/$rpc/…v1.Messaging/PullMessages"
// (optional) PrewarmReceiverURL, ListIdentitiesURL, GetFiUserStandingURL
```
`SendMessageURL` / `AckMessagesURL` (+`…Google` clients6 variants) and
`SignInGaiaURL` / `RegisterRefreshURL` already exist and are reused unchanged
[LIBGM].

Auth carried on EVERY Messaging/Registration RPC once signed in [AUTH §2.6, SEQ]:
Tachyon `tachyonAuthToken` **inside the request body** (not a header); Google
cookies + `Authorization: SAPISIDHASH <ts>_<sha1(ts+" "+SAPISID+" "+origin)>`
(origin `https://messages.google.com`); `x-goog-api-key: <GoogleAPIKey>`;
`x-user-agent: grpc-web-javascript/0.1`; content framing = PBLite
(`application/json+protobuf`).

---

# 3. Per-action call sequences [SEQ]

All method names below are [OBSERVED] unless tagged [BUNDLE] (in the JS descriptor
table but timing not observed) or [INFERRED].

**(1) Cold start / sign-in.** `Registration/SignInGaia` (2-step: initial then
get-token [INFERRED]) → `MessagesMultiDevice/GetFiUserStanding` →
`Registration/ListIdentities` → `Registration/RegisterRefresh` [BUNDLE] (mint/
refresh token + register Web-Push endpoint) → `Messaging/PrewarmReceiver`
[BUNDLE, optional] → open receive. Backbone SignInGaia→GetFiUserStanding→
ListIdentities→receive is OBSERVED; interleave of RegisterRefresh/PrewarmReceiver
is INFERRED.

**(2) Open receive.** `Messaging/PullMessages` opened once after sign-in; request
carries RequestHeader (auth) + (INFERRED) a resume cursor. HTTP response stays
open for minutes streaming batches; on completion the client immediately re-arms
a fresh `PullMessages` with advanced cursor (re-issue is RARE because each call is
long-lived). Keep-alive via HTTP chunk/heartbeat frames; whether the modern
client also sends periodic `NOTIFY_DITTO_ACTIVITY` was NOT observed.

**(3) Message arrives while hidden.** `←PullMessages` batch delivers on the open
stream (no new outbound needed to receive) → `→AckMessages` (delivery ack,
advances cursor; NOT a read receipt) → sometimes `→SendMessage` (purpose
unproven). Client keeps the message **UNREAD** and **the phone STILL notifies**.

**(4) Foreground (hidden→visible).** Exactly one `→SendMessage` (active-device /
foreground assertion; libgm analogue = `SetActiveSession()` =
`ActionType.GET_UPDATES(16)` SendMessage) → `→AckMessages`. This pair is the
tightest correlate of notification behavior.

**(5) Background (visible→hidden).** Client sends **NOTHING**. Stream stays open;
client simply stops asserting foreground.

**(6) Send a message.** `→SendMessage` with `OutgoingMessage.payload=12` =
encrypted envelope (recipient/conv/text; `ActionType.SEND_MESSAGE(3)` inside) →
`←SendMessageResponse` (id/timestamp not read) → echo returns down the open
`PullMessages` stream → `AckMessages`'d as in (3). No separate typing/
CreateConversation for a plain send (`SmartMessaging/CreateConversation` is
new-thread-only [BUNDLE]).

**(7) Acking.** `→AckMessages` = transport-level delivery ack that advances the
receive cursor. Fires after every received batch (3) and alongside the foreground
assertion (4). A READ RECEIPT is a DIFFERENT op: a `SendMessage` carrying
`ActionType.MESSAGE_READ(10)` [SOURCE/INFERRED], not `AckMessages`.

**(8) List conversations / contacts / messages.** No distinct observed method
names. In the libgm family these are `SendMessage` calls carrying list/GET
actions in the encrypted body (`LIST_CONVERSATIONS`, `LIST_CONTACTS`,
`LIST_MESSAGES`, `GET_UPDATES`), results delivered back down `PullMessages` and
paged via further `SendMessage`s. On the wire, "load conversations" ≈
`SendMessage`+`PullMessages`(delivery)+`AckMessages`, indistinguishable by method
name from a normal send [INFERRED — encrypted action bytes not captured].

**(9) Reconnect / auth refresh.** On stream drop: re-arm `→PullMessages` with last
cursor (maybe preceded by `PrewarmReceiver`). On 401-class: `→RegisterRefresh`
(mint fresh token, ECDSA-signed) then retry PullMessages. On hard failure:
`→SignInGaia` re-register. Bundle carries
`Start/StopFallbackToTachyonPullMessages` counters → client can fall back between
`PullMessages` and legacy `ReceiveMessages` [BUNDLE]. Only the PullMessages
re-issue timing is OBSERVED; escalation is INFERRED.

---

# 4. Auth/registration delta vs libgm + notification-routing suspects

## 4.1 What libgm already has and reuses unchanged [LIBGM (c)(d)(e)]

`SignInGaia` (Registration service, 2-step, `pair_google.go`), `RegisterRefresh`
(token refresh + optional `messages_web` Web-Push registration), `AuthData` +
`RequestCrypto` (AES-CTR payload cipher), `buildMessage`/`sendMessage*`,
`queueMessageAck`/`sendAckRequest`, `dittoPinger`, `LongPollingPayload` framing,
`decryptInternalMessage`/`HandleRPCMsg`/`handleUpdatesEvent`. The receive envelope
(`IncomingRPCMessage`/`RPCMessageData`/`BugleRoute`/`ActionType`) is very likely
byte-identical for PullMessages — reuse until a capture disproves it.

## 4.2 The delta to add (receive path only) [LIBGM (e)]

1. Endpoint constants `PullMessagesURL(+Google)`.
2. `PullMessagesRequest` proto — layout UNKNOWN; start by cloning
   `ReceiveMessagesRequest` (auth + `UnknownEmptyObject` filler), refine against a
   capture.
3. `doPullMessages(...)` loop = the current `doLongPoll` loop but building
   `PullMessagesRequest` and POSTing to `PullMessagesURL`; keep `readLongPoll`
   (`[[ … ]]` pblite dispatch) verbatim, fork only if the modern framing differs.
4. A `Client.UsePullMessages` selector gating `Connect()` between the two paths
   (mirror the existing `DontMarkActive` flag).
5. Possibly new `ActionType`/`responseType` entries or standalone
   `GetFiUserStanding`/`ListIdentities` RPC wrappers — TBD without a decrypted
   capture.
NOT required: no new crypto, no `buildMessage` change, no ack/pinger/fan-out
change.

## 4.3 Registration differences vs modern web [AUTH §4]

| aspect | libgm | modern web | conf |
|---|---|---|---|
| receive verb | `Messaging/ReceiveMessages` | `Messaging/PullMessages` | OBSERVED |
| device descriptor | `BrowserDetails{deviceType:TABLET(2), browserType:OTHER, OS:"libgm"}` | `{deviceType:WEB(1), real browserType, real OS}` | SOURCE/INFERRED |
| Fi standing / identity list | absent | `GetFiUserStanding` / `ListIdentities` | OBSERVED |
| Web-Push | can register `type:"messages_web"` | real web-push | SOURCE/INFERRED |
| auth | Tachyon token in body + cookies + SAPISIDHASH | same | SOURCE |

## 4.4 Fields SUSPECTED to control phone-notification routing (ranked)

1. **`BrowserDetails.deviceType` (WEB vs TABLET) + `browserType`** — libgm
   deliberately mislabels itself `TABLET`+`OTHER`+`OS:"libgm"`; real client sends
   `WEB`+real browser. Clearest "what kind of companion am I" descriptor.
   [INFERRED, medium — AUTH §5.1]
2. **Tachyon `capsArray`** (`GTPRegisterData.capsArray` /
   `GTPUserRegistrationState.capsArray`, fields 9/3 in the ancestor proto [PRIOR
   §3.1]) — an explicit per-device capability bitset broadcast to all of the
   user's devices; the natural place to encode "SMS-capable / companion". **Not
   present in libgm's Messages-specific proto** → libgm may omit caps the modern
   client sets. **Strongest STRUCTURAL candidate.** [INFERRED, medium — AUTH §5.2]
3. **`RegisterRefresh` Web-Push registration** (`PushRegistration{url,p256dh,
   auth}`) — but libgm can register `messages_web` push and the phone STILL
   notifies, so this alone is not the switch. [SOURCE, low — AUTH §5.3]
4. **Receive transport** (persistent `ReceiveMessages` bind vs catch-up
   `PullMessages`) — holding a bind may mark the device "reachable" differently.
   Behavioral, not a descriptor field. [INFERRED, low-med — AUTH §5.4]
5. **Runtime foreground assertion** (`SendMessage` on hidden→visible, §3(4)) — the
   observed data shows silence when backgrounded + phone keeps notifying;
   **presence, not a static registration field, is the tightest correlate.**
   [OBSERVED, medium — AUTH §5.5]

> **Contradiction across the docs on the notification lever:** `04` §5 and `05`
> lean toward **presence** (the foreground `SendMessage`) as the real driver;
> `04` §5.2 nominates the static **`capsArray`** as the strongest structural
> candidate. These are not reconciled — the honest position is that **no single
> field is proven** (all registration payloads were encrypted [AUTH §6]).

---

# 5. CONFIDENCE & GAPS

## 5.1 EVIDENCED (HIGH)
- Full RPC catalog: names, services, unary-vs-streaming mode, req/resp type
  identifiers [JS descriptors, verbatim].
- `PullMessages` is declared **unary** by BOTH [JS] and the GMSCore decompile
  [PRIOR].
- Every wrapper `header` field number + type (PullMessages req/resp header=1,
  SendMessage msg=2/hdr=3, OutgoingMessage payload=12, AckMessages header=1,
  SignInGaia resp field3, Identity type=1/id=2) [JS].
- libgm's complete `ReceiveMessages`-era envelope, enums, SignInGaia body, and
  active-session primitives [LIBGM] — the reusable substrate.
- The observed per-action sequences by METHOD NAME + timing (cold start, foreground
  pair, background silence, hidden-arrival ack, phone-still-notifies) [OBSERVED].
- Modern web declares `BrowserDetails.deviceType:WEB` vs libgm's `TABLET`
  [SOURCE].

## 5.2 INFERRED (MED / LOW)
- MED: PullMessages is a **hanging long-poll unary** (reconciles unary-declaration
  with minutes-open behavior); the receive body reuses libgm's
  `LongPollingPayload`/`IncomingRPCMessage` framing; `_.Gz` header decode;
  ReceiveMessages field-1 = header(JS)/auth(libgm) being the same bytes.
- LOW: PullMessagesRequest carries a Tachyon-style `startTimestamp`-like cursor
  (from `GTPInboxPullRequest`); PullMessagesResponse has `repeated messages`
  (from `GTPInboxPullResponse`); modern encrypted bodies reuse libgm `ActionType`
  numbers; the SignInGaia two-step; RegisterRefresh/PrewarmReceiver interleave.

## 5.3 UNKNOWN — needs live capture to confirm
- **Every PullMessages body field number/type below the header** (the repeated
  message field, the cursor/ack-state field) — the schema-less JS decode never
  names them and the payload was encrypted. **This is the #1 blocker.**
- The exact **receive stream framing** for modern PullMessages (`[[ … ]]` pblite
  chunk array like ReceiveMessages, or a new format).
- `AckMessages` ack-entry list layout; `SendMessage` recipient/conversation fields
  inside `payload=12`; the `ActionType` each observed `SendMessage` carries.
- `PrewarmReceiver`, `Echo`, `GetFiUserStanding`, `ListIdentities` **bodies**
  (wrappers only; no prior art for GetFiUserStanding/ListIdentities at all).
- `ResponseHeader (_.Fz)` internals (no getters anywhere).
- Whether the modern client sends periodic `NOTIFY_DITTO_ACTIVITY`.
- Whether the sometimes-present hidden-arrival `SendMessage` (§3(3)) is distinct
  from the foreground assertion (§3(4)).
- Which registration field (if any) actually controls phone-notification routing.

## 5.4 CONTRADICTIONS between sources (unresolved, called out, NOT smoothed)
1. **Transport:** [JS]+[PRIOR] say `PullMessages` is **unary**; [OBSERVED]/[SEQ]/
   `03`/`04` describe/label it **server-streaming** (behaviorally). Working model:
   unary long-poll. §1.1.
2. **SignInGaia service prefix:** [OBSERVED] grouped it under `Messaging`; every
   SOURCE puts it on `Registration`. Source wins (names-only capture). §1.6.
3. **GetFiUserStanding service:** [JS] descriptors put it on
   `MessagesMultiDevice`; [OBSERVED]/[SEQ] loosely grouped it under `Messaging`.
   §1.6.
4. **ReceiveMessages field 1:** [JS] = RequestHeader (`_.fz`); [LIBGM] =
   AuthMessage. Likely the same bytes, two schemas. §1.4.
5. **Notification lever:** `04`/`05` favor runtime **presence**; `04` §5.2 favors
   static **capsArray**. Unreconciled; nothing proven. §4.4.
6. **GMSCore vs live streaming:** the decompile's unary `PullMessages` may predate
   a streaming variant, OR the long-lived socket is HTTP-chunked not a gRPC
   server-stream [PRIOR headline #2]. Unresolved.

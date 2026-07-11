# Prior Art: Modern Google Messages / instantmessaging v1 Messaging API (PullMessages, SignInGaia, GetFiUserStanding)

Scope: what open-source / reverse-engineered material documents the **modern** web-client RPCs observed live this session
(`SignInGaia`, `GetFiUserStanding`, `ListIdentities`, `SendMessage`, `PullMessages`, `AckMessages` on
`instantmessaging-pa.googleapis.com/$rpc/google.internal.communications.instantmessaging.v1.Messaging/<Method>`), and how
they relate to the older `ReceiveMessages` path that mautrix/gmessages (libgm) uses.

Confidence legend: **[HIGH]** = verbatim from source; **[MED]** = strong inference from source; **[LOW]** = speculation.
Every schema/claim cites a URL.

---

## TL;DR / headline findings

1. **No open-source implementation uses `PullMessages` for this service.** A broad GitHub code search for `PullMessages`
   returns only unrelated projects (ONVIF cameras, Cloud Pub/Sub, NATS, dapr, etc.). The only place
   `instantmessaging.v1.Messaging/PullMessages` appears is **decompiled Google Play Services (GMSCore)**. Every live
   third-party client (libgm and all its ports) receives via **`ReceiveMessages`**, never `PullMessages`. **[HIGH]**
2. In decompiled GMSCore, the `Messaging` service registers **seven** methods; `PullMessages` is **UNARY**, whereas
   `ReceiveMessages`/`ReceiveMessagesExpress` are **SERVER_STREAMING** and `Bind` is **BIDI_STREAMING**. This *contradicts*
   the live observation that modern-web `PullMessages` stays open for minutes as a server stream — so either the decompile
   predates a streaming `PullMessages`, or the long-lived socket is HTTP-level (chunked) rather than a gRPC server-stream. **[HIGH for the decompile; the contradiction is noted]**
3. `SignInGaia` in every source (libgm + GMSCore decompile) lives on the **`...v1.Registration`** service on the
   **`instantmessaging-pa.clients6.google.com`** host — *not* on `...v1.Messaging`. The live capture put it on the
   `Messaging` path; that is a genuine divergence of the modern client from documented prior art. **[HIGH]**
4. **`GetFiUserStanding` has zero prior art** anywhere on GitHub (Google Fi standing check). No schema exists publicly. **[HIGH]**
5. **`ListIdentities`** also has no prior art on the instantmessaging Messaging service (the name is generic and only
   collides with unrelated projects). **[MED]**
6. The "active session / foreground" primitives already exist in libgm: `ActionType` **`NOTIFY_DITTO_ACTIVITY=22`**,
   **`BROWSER_PRESENCE_CHECK=11`**, **`ACK_BROWSER_PRESENCE=17`**, and `SetActiveSession()` (which is a `GET_UPDATES=16`
   SendMessage). These are the mechanism by which a web/companion client asserts activity — the plausible lever behind
   phone-notification suppression. **[HIGH that they exist; MED that they drive suppression]**

---

## 1. mautrix/gmessages — libgm (canonical open-source reference, Go)

Repo: <https://github.com/mautrix/gmessages> (libgm under `pkg/libgm`). This is the implementation the task's "older path" refers to.

### 1.1 Endpoint inventory — `pkg/libgm/util/paths.go` **[HIGH]**
Source: <https://raw.githubusercontent.com/mautrix/gmessages/main/pkg/libgm/util/paths.go>

- Hosts: `instantMessagingBaseURL = https://instantmessaging-pa.googleapis.com`,
  `instantMessagingBaseURLGoogle = https://instantmessaging-pa.clients6.google.com` (the `.clients6.` host the task also saw).
- **Messaging** service (`/$rpc/google.internal.communications.instantmessaging.v1.Messaging`): only
  `ReceiveMessages`, `SendMessage`, `AckMessages` (each with a `...Google` variant on the clients6 host). **No PullMessages, no ListIdentities, no GetFiUserStanding.**
- **Pairing** service (`...v1.Pairing`): `RegisterPhoneRelay`, `RefreshPhoneRelay`, `GetWebEncryptionKey`, `RevokeRelayPairing`.
- **Registration** service (`...v1.Registration`, on the clients6 host): `SignInGaia`, `RegisterRefresh`.

So in libgm, `SignInGaia` = Registration service, and the receive path = `Messaging/ReceiveMessages`.

### 1.2 RPC envelope — `pkg/libgm/gmproto/rpc.proto` **[HIGH]**
Source: <https://raw.githubusercontent.com/mautrix/gmessages/main/pkg/libgm/gmproto/rpc.proto>

The long-poll receive stream is decoded as a sequence of `LongPollingPayload`:
```proto
message LongPollingPayload {
    optional IncomingRPCMessage data = 2;
    optional util.EmptyArr heartbeat = 3;
    optional StartAckMessage ack = 4;   // StartAckMessage { optional int32 count = 1; }
    optional util.EmptyArr startRead = 5;
}
message IncomingRPCMessage {
    string responseID = 1;
    BugleRoute bugleRoute = 2;          // enum below
    uint64 startExecute = 3;
    MessageType messageType = 5;
    uint64 finishExecute = 6;
    uint64 microsecondsTaken = 7;
    authentication.Device mobile = 8;
    authentication.Device browser = 9;
    bytes messageData = 12;             // RPCMessageData or RPCPairData
    string signatureID = 17;
    string timestamp = 21;
    GDittoSource gdittoSource = 23;     // { int32 deviceID = 2; }
}
message OutgoingRPCMessage {           // what SendMessage/AckMessages POST
    message Auth { string requestID = 1; bytes tachyonAuthToken = 6; authentication.ConfigVersion configVersion = 7; }
    message Data { string requestID = 1; BugleRoute bugleRoute = 2; bytes messageData = 12; Type messageTypeData = 23; }
    authentication.Device mobile = 1;
    Data data = 2;
    Auth auth = 3;
    int64 TTL = 5;
    repeated string destRegistrationIDs = 9;
}
message OutgoingRPCData { string requestID = 1; ActionType action = 2; bytes unencryptedProtoData = 3; bytes encryptedProtoData = 5; string sessionID = 6; }
```

`BugleRoute` enum: `Unknown=0`, **`DataEvent=19`** (encrypted RCS/SMS "bugle" path), **`PairEvent=14`**, **`GaiaEvent=7`**
(Google-account / SignInGaia path — no phone relay). A SignInGaia-registered device rides `GaiaEvent`. **[HIGH]**

`MessageType` enum: `UNKNOWN=0`, `BUGLE_MESSAGE=2`, `GAIA_1=3`, `BUGLE_ANNOTATION=16`, `GAIA_2=20`. **[HIGH]**

`ActionType` enum (the semantic op carried inside the encrypted `SendMessage`/receive payloads) — 50+ values; ones relevant
to activity/presence and to the observed foreground assertion:
`GET_UPDATES=16`, `BROWSER_PRESENCE_CHECK=11`, `ACK_BROWSER_PRESENCE=17`, `NOTIFY_DITTO_ACTIVITY=22`, `IS_BUGLE_DEFAULT=31`,
`MESSAGE_READ=10`, `SEND_MESSAGE=3`, `PREWARM=48`, plus the full GAIA-pairing set (`GET_DEVICES_AVAILABLE_FOR_GAIA_PAIRING=41`,
`CREATE_GAIA_PAIRING=42/44/45`, `UNPAIR_GAIA_PAIRING=46`, `CANCEL_GAIA_PAIRING=47`), `LINK_RCS_IDENTITY=50`, `UNLINK_RCS_IDENTITY=51`. **[HIGH]**

### 1.3 SignInGaia schema — `pkg/libgm/gmproto/authentication.proto` **[HIGH]**
Source: <https://raw.githubusercontent.com/mautrix/gmessages/main/pkg/libgm/gmproto/authentication.proto>
```proto
message SignInGaiaRequest {
    message Inner {
        message DeviceID { int32 unknownInt1 = 1; /*3*/ string deviceID = 2; /* messages-web-{uuid no dashes} */ }
        message Data    { bytes someData = 3; /* maybe an encryption key */ }
        DeviceID deviceID = 1;
        Data someData = 36 [(pblite.pblite_binary) = true];
    }
    AuthMessage authMessage = 1;
    Inner inner = 2;
    int32 unknownInt3 = 3;
    string network = 4;
}
message SignInGaiaResponse {
    message Header { uint64 unknownInt2 = 2; int64 unknownTimestamp = 4; }
    message DeviceData {
        message DeviceWrapper { Device device = 1; }   // Device { int64 userID=1; string sourceID=2; string network=3; }
        DeviceWrapper deviceWrapper = 1;
        repeated RPCGaiaData.UnknownContainer.Item2.Item1 unknownItems2 = 2;
        repeated RPCGaiaData.UnknownContainer.Item4 unknownItems3 = 3;
    }
    Header header = 1;
    string maybeBrowserUUID = 2 [(pblite.pblite_binary) = true];
    DeviceData deviceData = 3;
    TokenData tokenData = 4;
}
```
Called via `SignInGaiaURL = registrationBaseURL + "/SignInGaia"` (`pkg/libgm/pair_google.go`,
`baseSignInGaiaPayload()`), content-type PBLite. **[HIGH]**

### 1.4 Receive / Ack / activity messages — `pkg/libgm/gmproto/client.proto` **[HIGH]**
Source: <https://raw.githubusercontent.com/mautrix/gmessages/main/pkg/libgm/gmproto/client.proto>
```proto
message NotifyDittoActivityRequest { bool success = 2; }   // field 2; not really a bool
message NotifyDittoActivityResponse {}
message ReceiveMessagesRequest {
    authentication.AuthMessage auth = 1;
    message UnknownEmptyObject2 { UnknownEmptyObject1 unknown = 2; }   // field 4 wrapper (see source)
}
message AckMessageRequest {
    message Message { string requestID = 1; authentication.Device device = 2; }
    authentication.AuthMessage authData = 1;
    // repeated Message acks ...
}
```

### 1.5 "Active session" behaviour (the notification-suppression lever) **[HIGH code; MED semantics]**
Source: `pkg/libgm/methods.go`
<https://raw.githubusercontent.com/mautrix/gmessages/main/pkg/libgm/methods.go>,
`pkg/connector/handlegmessages.go`
<https://raw.githubusercontent.com/mautrix/gmessages/main/pkg/connector/handlegmessages.go>,
`pkg/libgm/longpoll.go`
<https://raw.githubusercontent.com/mautrix/gmessages/main/pkg/libgm/longpoll.go>

- `SetActiveSession()` resets the session ID and sends an `ActionType_GET_UPDATES` SendMessage (`OmitTTL`). The bridge calls
  it on connect and when "reactivating" — i.e. this is libgm's equivalent of the modern client's foreground/active
  assertion. The modern web client's single `SendMessage` on hidden→visible is the analogue.
- `NotifyDittoActivity()` (`NOTIFY_DITTO_ACTIVITY`, `{success:true}`) is the periodic phone keep-alive ("ditto pinger",
  default every 1 min, backoff to 64 min) that also detects "phone not responding". This is the health-ping, distinct from
  the foreground assertion.
- `ackBrowserPresence()` sends `ACK_BROWSER_PRESENCE`; `BROWSER_PRESENCE_CHECK` is the inbound presence probe.
- Interpretation: libgm's model is "one active web session; the phone treats the active session as the presentation
  surface." This is the documented hook most consistent with the task's hypothesis that being on a particular receive
  path / registration changes whether the phone keeps notifying. Whether `PullMessages`+`SignInGaia`-on-Messaging registers
  as a *non-suppressing companion* is **not** answerable from libgm (libgm doesn't implement that path). **[MED]**

---

## 2. Decompiled Google Play Services (GMSCore) — the only source that names `PullMessages`

### 2.1 `Romern/gms_decompiled` **[HIGH]**
Repo: <https://github.com/Romern/gms_decompiled> (deobfuscated method names are Google's; the type names `cb**` are obfuscated).

`Messaging` service method registrations (gRPC `MethodDescriptor`s), from `sources/p000/`:
- **`PullMessages`** — `chtu.UNARY`, req `cbko`, resp `cbkp` — `sources/p000/bcmr.java`
  <https://raw.githubusercontent.com/Romern/gms_decompiled/master/sources/p000/bcmr.java>
- **`ReceiveMessages`** — `SERVER_STREAMING`, req `cbkr`, resp `cbkx` — `sources/p000/bcmt.java` and `aitg.java`
  <https://raw.githubusercontent.com/Romern/gms_decompiled/master/sources/p000/bcmt.java>
- **`ReceiveMessagesExpress`** — `SERVER_STREAMING`, req `cbkq`, resp `cbkx` — `sources/p000/aitb.java`
- **`SendMessage`** — `UNARY`, req `cbkk`, resp `cbkl` — `aitg.java` / `azhs.java`
- **`SendMessageExpress`** — `UNARY`, req `cbll`, resp `cblm` — `aitb.java`
- **`AckMessages`** — `UNARY`, req `cbjk`, resp `cbjl` — `sources/p000/bclz.java`
  <https://raw.githubusercontent.com/Romern/gms_decompiled/master/sources/p000/bclz.java>
- **`Bind`** — `BIDI_STREAMING`, req `cbjt`, resp `cbka` — `sources/p000/azet.java`

`Registration` service: **`SignInGaia`** — `sources/p000/bcnh.java`
(`chtv.m149567a("google.internal.communications.instantmessaging.v1.Registration", "SignInGaia")`). Confirms SignInGaia is
Registration-service, matching libgm §1.1. **[HIGH]**

Obfuscated request/response type bodies (`cbko`/`cbkp` etc.) were not decoded here; extracting their field numbers would
require pulling the corresponding `cb**.java` message classes (a follow-up if a `PullMessages` schema is needed).

### 2.2 `wangxinqing/PrebuiltGmsCorePano01` **[HIGH]** (corroboration, different GMS build)
Source: `sources/defpackage/uti.java`, `utd.java`
<https://github.com/wangxinqing/PrebuiltGmsCorePano01/blob/master/sources/defpackage/uti.java>
Same `Messaging` service with `ReceiveMessages`/`ReceiveMessagesExpress` (SERVER_STREAMING) and
`SendMessage`/`SendMessageExpress` (UNARY). (This build's snippet did not surface a `PullMessages` line, consistent with
`PullMessages` being a newer/less-used method than `ReceiveMessages`.)

---

## 3. Older-generation ancestor of "Pull": Allo/Tachyon `GTPInbox*` protos

Repo: `avaidyam/GoogleAPIProtobufs` (re-synthesized Allo/Hangouts/Tachyon protos)
Source: <https://github.com/avaidyam/GoogleAPIProtobufs/blob/master/google.internal.communications.instantmessaging.v1.proto> **[MED — different generation, but same package name and a Pull/Send/Ack triad]**

Contains an **inbox pull/ack** triad that is structurally what a `PullMessages` schema looks like — likely the direct
ancestor:
- `GTPInboxPullRequest { startTimestamp; GTPRequestHeader header }`
- `GTPInboxPullResponse { repeated ... messagesArray; GTPResponseHeader header }`
- `GTPInboxSendRequest { destId; GTPInboxMessage; header; timeToLive; sendAs }` → `GTPInboxSendResponse { header; timestamp }`
- `GTPInboxAckRequest { repeated messageIdsArray; header; clientStatus; repeated acksArray; sendAs }` → `GTPInboxAckResponse { header }`
- `GTPInboxMessage { messageId; messageType; timestamp; senderId; receiverId; groupId; <payload variants: fireball/tachyon/secure/basic/group/userdata> }`

Field numbers per-field were not all captured here; this file is the best public lead for the *shape* of Pull/Ack request
bodies (a `startTimestamp` cursor + header; ack by `messageIds` + `clientStatus`). Treat as a **model, not the exact v1 schema**. **[MED]**

---

## 4. Sibling public API: Tachyon "Express" (Nearby Share) — fully open schemas

`ReceiveMessagesExpress`/`SendMessageExpress` on the same `...v1.Messaging` service are used by Chromium Nearby Share and
have **public .proto**:
- Chromium: `chrome/browser/nearby_sharing/instantmessaging/proto/instantmessaging.proto`
  <https://github.com/Carbon-Browser/browser/blob/master/src/chrome/browser/nearby_sharing/instantmessaging/proto/instantmessaging.proto>
  — defines `ReceiveMessagesExpressRequest`, `InboxMessage`, `StreamBody`, etc. **[HIGH for Express variant]**
- `google/nearby` `internal/proto/messaging.proto`
  <https://github.com/google/nearby/blob/master/internal/proto/messaging.proto> — `rpc ReceiveMessagesExpress(...)`.

Useful because the *Express* request/response share the envelope family; the non-Express `PullMessages`/`ReceiveMessages`
bodies are the encrypted-transport cousins. **[MED for transfer to PullMessages]**

Also related: Chromium `remoting/proto/ftl/v1/ftl_services.proto` defines `rpc SignInGaia(SignInGaiaRequest) returns
(SignInGaiaResponse)` for the FTL/Tachyon signaling service — public confirmation of the SignInGaia registration pattern
(different service, same idea). <https://github.com/endlessm/chromium-browser/blob/master/remoting/proto/ftl/v1/ftl_services.proto> **[HIGH]**

---

## 5. Other third-party implementations (all use `ReceiveMessages`, none use `PullMessages`)

Confirms the ecosystem baseline; each is a libgm-family port and thus documents the same three Messaging methods:
- `imbackwithrampage/libgmessages` (Go fork) — `pb/google_messages.proto`, `client/consts.go`. <https://github.com/imbackwithrampage/libgmessages>
- `pixelmonaskarion/grust` (Rust) — `src/consts.rs` `MESSAGING_BASE_URL`. <https://github.com/pixelmonaskarion/grust>
- `mweinbach/swift-gmessages` (Swift) — `Sources/LibGM/Models/Constants.swift`. <https://github.com/mweinbach/swift-gmessages>
- `vayun-mathur/Modern-Apps` (Kotlin) — `messages/src/main/proto/*.proto` (incl. `authentication.proto` with
  `SignInGaiaRequest/Response`) and `Constants.kt`. Closest to a fresh re-proto but still ReceiveMessages-based. <https://github.com/vayun-mathur/Modern-Apps>
- `KTibow/message` (TS) — `index.ts` posts `SendMessage`/`ReceiveMessages`. <https://github.com/KTibow/message>
- `danhol86/dh-messages-api` (Go + JS) — posts to `ReceiveMessages`/`SendMessage`/`AckMessages`. <https://github.com/danhol86/dh-messages-api>
- `EionRobb/purple-googlemessages` (C, libpurple) — `GOOGLEMESSAGES_MESSAGING_BASE`. <https://github.com/EionRobb/purple-googlemessages>
- `Offline-DC/matrix-app` (Kotlin) — `GMGaiaPairing.kt` uses `Messaging/ReceiveMessages` + `Messaging/SendMessage`. <https://github.com/Offline-DC/matrix-app>
- `Mause/gbooks-upload` (Python) — `endpoints.yaml` lists the service with `ReceiveMessages/SendMessage/AckMessages`
  (Messaging) and `RegisterPhoneRelay/RefreshPhoneRelay/GetWebEncryptionKey/RevokeRelayPairing` (Pairing), plus a
  `SignInGaia` entry. <https://github.com/Mause/gbooks-upload/blob/master/src/google_internal_apis/endpoints.yaml>

Also useful low-signal corroboration that `instantmessaging-pa.googleapis.com` is the always-on Android messaging transport:
pi-hole/AdGuard discussions. <https://discourse.pi-hole.net/t/smartphone-is-flooting-instantmessaging-pa-googleapis-com/26919>

---

## 6. Gaps / where prior art runs out (do NOT fabricate)

- **`PullMessages` request/response field schema**: not public. Only the method registration (UNARY, obfuscated types
  `cbko`→`cbkp`) in GMSCore decompile, plus the `GTPInboxPull*` ancestor shape in §3. Would require decoding
  `Romern/gms_decompiled` `cbko`/`cbkp` message classes to get field numbers.
- **`GetFiUserStanding`**: no prior art at all. Google Fi standing check; schema unknown.
- **`ListIdentities`**: no prior art on this service.
- **Streaming type of modern `PullMessages`**: decompile says UNARY; live capture behaves like a long-lived stream —
  unresolved (§ headline #2).
- **Whether the modern PullMessages/SignInGaia-on-Messaging path is what stops phone-notification suppression**:
  unverified. The only documented suppression-adjacent primitives are the `NOTIFY_DITTO_ACTIVITY` / `GET_UPDATES`
  active-session / `*_BROWSER_PRESENCE` set in libgm (§1.5).

---

## Most useful sources (ranked)
1. libgm proto+code (envelope, enums, SignInGaia, active-session): <https://github.com/mautrix/gmessages/tree/main/pkg/libgm>
2. GMSCore decompile naming `PullMessages`/`Bind` + streaming types: <https://github.com/Romern/gms_decompiled> (`sources/p000/bcmr.java`, `bcmt.java`, `bclz.java`, `bcnh.java`, `azet.java`, `aitg.java`)
3. Allo/Tachyon `GTPInboxPull*` ancestor schema: <https://github.com/avaidyam/GoogleAPIProtobufs/blob/master/google.internal.communications.instantmessaging.v1.proto>
4. Chromium Nearby `instantmessaging.proto` (Express variant + FTL SignInGaia): <https://github.com/Carbon-Browser/browser/blob/master/src/chrome/browser/nearby_sharing/instantmessaging/proto/instantmessaging.proto>
5. Endpoint inventory YAML: <https://github.com/Mause/gbooks-upload/blob/master/src/google_internal_apis/endpoints.yaml>

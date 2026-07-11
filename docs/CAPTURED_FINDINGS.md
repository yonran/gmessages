# CAPTURED_FINDINGS.md — live-capture-backed correction to the modern-API spec

Supersedes the speculative parts of `MODERN_API.md` and `IMPLEMENTATION_NOTES.md`
wherever they conflict. Everything here is grounded in decrypted live JSPB bodies
(`/Users/yonran/third-party/gmcap/rpc/<Method>-<id>.network-{request,response}`)
cross-referenced against libgm gmproto. Derived in `docs/re/CAP-01…03`.

**REDACTION RULE (obeyed throughout):** every real token, phone number, E.164,
account/GAIA id, email, message/request id, session id, and base64 key blob is
replaced by a typed placeholder `<type:name>`. Only STRUCTURE (JSPB array index →
proto field number → wire type → meaning) is documented. No live secret appears
below.

---

## 1. CORRECTED ARCHITECTURE (read this first)

The earlier spec was built before payloads could be decrypted and guessed wrong
about the receive path. The capture corrects three things:

1. **Modern receive == `ReceiveMessages` == what libgm already does.** The modern
   `messages.google.com/web` client's real inbound message data arrives on
   **`Messaging/ReceiveMessages`** (`ReceiveMessages-56`, a ~103 KB stream of
   `IncomingRPCMessage` frames). The request body is `[auth, null, null, []]` —
   auth at field 1, empty object at field 4 — **structurally the same call libgm
   already implements.** The inbound frames are `rpc.IncomingRPCMessage`
   **field-for-field**. libgm is already on the correct receive endpoint.

2. **`PullMessages` is a heartbeat, not the receive path.** Every captured
   `PullMessages` response (`PullMessages-80/85/113`) is `[null,null,1]` — a
   `LongPollingPayload` heartbeat (field 3 = 1) carrying **zero message data**. It
   runs on the RCS `-jms-us` host as a keep-alive long-poll. **Implementing
   PullMessages would not change message receipt at all.** This directly refutes
   the `MODERN_API.md` premise that PullMessages is "the modern receive path".

3. **The notification lever is REGISTRATION, not the receive verb.** Because both
   clients receive over the same endpoint, the difference in phone-notification
   behavior (modern web keeps the phone notifying vs. libgm can suppress it) is
   set at **pair time in `BrowserDetails`**, not in how messages are pulled. The
   `SignInGaia` device descriptor is byte-identical between the two clients, so it
   is ruled out; the lever is `BrowserDetails.deviceType`.

**Net:** stop building a parallel PullMessages receive path. The high-value work
is a one-field registration change.

---

## 2. CAPTURE-BACKED PROTO SCHEMAS

Confidence is now **HIGH** for every field number the live capture confirms
(JSPB array index + 1 = proto field number). Divergences from libgm are flagged.

### 2.1 RequestHeader ( == `authentication.AuthMessage` ) — HIGH

First element of nearly every request.
`[ <string:requestId>, null, <string:network>, null, null, <bytes:tachyonAuthToken|null>, <ConfigVersion> ]`

```proto
message RequestHeader {              // identical to authentication.AuthMessage
  string requestId            = 1;   // idx0, uuidv4
  string network              = 3;   // idx2: "GDitto" | "RCS" | "CMS"
  bytes  tachyonAuthToken     = 6;   // idx5: base64; TOKEN LIVES AT FIELD 6
  ConfigVersion configVersion = 7;   // idx6
}
message ConfigVersion { int32 Year=3; int32 Month=4; int32 Day=5; int32 V1=7; int32 V2=9; }
```
- `tachyonAuthToken` (field 6) is present only on token-authed calls
  (ReceiveMessages, SendMessage, AckMessages, PullMessages, LookupRegistered);
  `null` on the cookie/GAIA-authed pre-registration calls (SignInGaia,
  ListIdentities, GetFiUserStanding).
- **No divergence from libgm** — `AuthMessage`/`ConfigVersion` match byte-for-byte.
  This resolves `MODERN_API.md §1.7`'s open question: the token is carried in the
  header body at field 6, not a separate scheme.

### 2.2 SignInGaia (network "GDitto") — HIGH

**Request** `[SignInGaia-46]`
`[ RequestHeader(token=null), [[3,"messages-web-<hex:sessionId>"]], 1, "GDitto" ]`
```proto
message SignInGaiaRequest {
  RequestHeader authMessage = 1;              // idx0, token null (pre-reg)
  message Inner {
    message DeviceID { int32 unknownInt1 = 1; string deviceID = 2; } // [3, "messages-web-<hex32>"]
    DeviceID deviceID = 1;
    // Data someData = 36;   // present only on the get-token 2nd call; absent here
  }
  Inner  inner       = 2;                      // idx1
  int32  unknownInt3 = 3;                      // idx2 = 1
  string network     = 4;                      // idx3 = "GDitto"
}
```
The leading `3` is `Inner.DeviceID.unknownInt1` (a device-registration
discriminator), **NOT** the `DeviceType` enum. libgm emits this identically →
**not the lever.**

**Response** `[SignInGaia-46]` — the account device roster.
```proto
message SignInGaiaResponse {
  message Header { uint64 respId = 2; int64 tsMicros = 4; }
  Header     header          = 1;   // idx0
  string     maybeBrowserUUID = 2;  // idx1 = null here
  DeviceData deviceData      = 3;   // idx2
}
// DeviceData = 4-element array:
//  f1 deviceWrapper : [[16, "<string:email>", "GDitto"]]          (account identity, idType 16)
//  f2 summaryRows   : repeated [ <string:uuid>,null,null, <int:role f4>, <string:lang f5>, null, <uint64 f7> ]
//  f3 detailRows    : repeated [ <string:uuid>,null, <int:localFlag f3>, <int:deviceKind f4>, null,null, <int64:tsMicros f7>, <bytes:blob f8> ]
//  f4               : [ null, [[],[]] ]  (no observed data)
```
This roster is what governs phone routing. **DeviceType-relevant enum evidence**
(observed 4-device sample, redacted):

| device | f2.role (f4) | f3.localFlag (f3) | f3.deviceKind (f4) |
|---|---|---|---|
| local web (lang "en-US", blob `CAEQ…`) | 1 | 1 | 19 |
| remote (EC pubkey blob) | 6 | 6 | 6 |
| remote | 6 | 6 | 6 |
| remote (32-byte blob) | 6 | 6 | 19 |

So the server assigns a **web companion `role = 6`**. The two roster enum domains
are `localFlag {1=local, 6=remote}` and `deviceKind {6, 19}` — this **corrects
libgm's comment that deviceKind is "always 6"** (`19` also occurs). The `6`
almost certainly derives from the `BrowserDetails.deviceType` sent at pairing —
the field we can move (§3).

### 2.3 DeviceType enum (the pairing descriptor enum) — HIGH (from libgm proto)

```proto
enum DeviceType { UNKNOWN_DEVICE_TYPE = 0; WEB = 1; TABLET = 2; PWA = 3; }
enum BrowserType { UNKNOWN_BROWSER_TYPE=0; OTHER=1; CHROME=2; FIREFOX=3; SAFARI=4; OPERA=5; IE=6; EDGE=7; }
```
Note: this small pairing-descriptor `DeviceType` is a **different enum domain**
from the roster `{1,6,19}` integers in §2.2. `BrowserDetails` was not in these
particular captures (SignInGaia uses the compact `Device` descriptor), but the
enum values are HIGH from `authentication.proto`.

### 2.4 ListIdentities (network "RCS") — HIGH

**Req** `[ RequestHeader(token=null,"RCS"), [16,"<string:email>","RCS"] ]`
**Resp** `[ [null,<uint64:respId>], [[1,"<phone:e164>","RCS"]] ]`
```proto
message ListIdentitiesRequest  { RequestHeader header = 1; authentication.Device identity = 2; }
message ListIdentitiesResponse { message H{uint64 id=2;} H header=1; repeated authentication.Device identities=2; }
```
Resolves the account GAIA identity (`idType 16`) to its registered RCS phone
(`idType 1`). No libgm equivalent; RCS-path, **not a receive prerequisite**.

### 2.5 LookupRegistered (network "RCS") — HIGH

**Req** `[ RequestHeader(token,"RCS"), [[1,"<phone:e164>","RCS"]], null,null,null,null, 1 ]`
```proto
message LookupRegisteredRequest {
  RequestHeader header = 1;
  repeated authentication.Device lookup = 2;   // numbers to probe
  int32 unknownFlag = 7;                        // idx6 = 1
}
message LookupRegisteredResponse {
  message H{uint64 id=2; int64 ts=4;} H header=1;
  repeated authentication.Device queried = 3;
  message Result { authentication.Device device=1; int32 status=2; RcsFeatureTags capabilities=5; }
  repeated Result results = 4;                  // 3GPP RCS feature tags (rcse.im, fthttp, msgfallback…)
}
```
Per-recipient RCS reachability/capability probe. No libgm equivalent; RCS-path,
**not a receive prerequisite.**

### 2.6 AckMessages (network "GDitto") — HIGH — **DIVERGES from libgm**

**Req** `[ RequestHeader(token,"GDitto"), [ "<string:msgId>", "<string:msgId>", … ] ]`
**Resp** `[[null,<uint64>]]`
```proto
message AckMessagesRequest {
  RequestHeader header = 1;          // idx0
  repeated string messageIds = 2;    // idx1: FLAT list of message-request UUIDs
}
```
**Divergence:** libgm's `client.AckMessageRequest` models this as
`{ authData=1, EmptyArr emptyArr=2, repeated Message acks=4 }` with each
`Message = {requestID=1, Device device=2}`. The **modern capture is flatter**: a
bare `repeated string` at **field 2** — no `emptyArr`, no `Device` wrapper, no
field 4. A libgm-shaped ack would put the ids in the wrong field for the modern
endpoint. (Independent of the notification fix; note for correctness if the
modern ack path is ever exercised.)

### 2.7 SendMessage (network "GDitto") — HIGH — matches libgm

**Req** `[SendMessage-50]`
```proto
message SendMessageRequest {           // == rpc.OutgoingRPCMessage
  authentication.Device mobile = 1;    // [16,"<string:acct>","GDitto"]
  message Data {
    string     requestID   = 1;        // tmpID
    BugleRoute bugleRoute  = 2;        // 19 (DataEvent)
    bytes      messageData = 12;       // base64 OutgoingRPCData (encrypted)
    message Type { util.EmptyArr emptyArr = 1; MessageType messageType = 2; }
    Type       messageTypeData = 23;   // [null,2]
  }
  Data data = 2;
  AuthMessage auth = 3;                 // == RequestHeader
  int64 TTL = 5;                        // 86400000000
  repeated string destRegistrationIDs = 9;
}
message SendMessageResponse {           // == rpc.OutgoingRPCResponse
  message SomeIdentifier { string someNumber = 2; } SomeIdentifier someIdentifier = 1;
  optional string timestamp = 2;
}
```
**Matches `rpc.OutgoingRPCMessage`/`OutgoingRPCResponse` field-for-field.** libgm
sends correctly today.

### 2.8 ReceiveMessages (network "GDitto") — HIGH — THE inbound path (matches libgm)

**Req** `[ReceiveMessages-56]` `[ RequestHeader(token,"GDitto"), null, null, [] ]`
```proto
message ReceiveMessagesRequest {
  authentication.AuthMessage auth = 1;   // idx0
  UnknownEmptyObject unknown = 4;         // idx3 = []  (libgm nests [null,[]]; capture sends bare []; same field)
}
```
**Resp** = `[ [ entry, entry, … ] ]`; each entry is a `LongPollingPayload`:
data `[null,<IncomingRPCMessage>]` (field 2), heartbeat `[null,null,[]]` (field
3), or terminal `[1,"The operation was cancelled."]`.
```proto
message IncomingRPCMessage {            // == rpc.IncomingRPCMessage, field-for-field
  string responseID = 1; BugleRoute bugleRoute = 2; uint64 startExecute = 3;
  MessageType messageType = 5; uint64 finishExecute = 6; uint64 microsecondsTaken = 7;
  authentication.Device mobile = 8; authentication.Device browser = 9;
  bytes messageData = 12;               // encrypted OutgoingRPCData/RPCMessageData
  string signatureID = 17;
}
```
**This confirms inbound data arrives here, not on PullMessages.** libgm already
decodes exactly this frame.

### 2.9 PullMessages (network "RCS", `-jms-us` host) — HIGH — heartbeat only

**Req** `[ RequestHeader(token,"RCS"), [] ]` (header=1, empty `cursor`=2 — pins
the layout libgm left as a TODO; the second element is an empty array, **not** an
encrypted resume cursor).
**Resp** — every observed response is `[null,null,1]`, a heartbeat (field 3 = 1),
zero message data. **Implementing PullMessages would not change message receipt.**

---

## 3. REGISTRATION PATCH PLAN FOR libgm (ranked)

All in `pkg/libgm/util/config.go`, `BrowserDetailsMessage` — consumed at pair
time via `util.BrowserDetailsMessage` in `pair_google.go` (Gaia) and `pair.go`
(QR). `BrowserDetails` is **not** re-sent on `RegisterRefresh`, so changing it
requires **re-pairing**.

Current libgm vs. modern web:

| proto field | libgm value | modern web (inferred) |
|---|---|---|
| f2 `browserType` | `OTHER` (1) | `CHROME` (2) |
| f3 `OS` | `"libgm"` | `"Linux"`/`"Windows"`/`"Chrome OS"` |
| f6 `deviceType` | **`TABLET` (2)** | **`WEB` (1)** (or `PWA` (3)) |

Ranked changes:

1. **[HIGHEST VALUE — single field] `deviceType: TABLET(2) → WEB(1)`.** A paired
   "tablet" reads to Google as a standalone device that owns the thread and
   suppresses the phone; `messages-web` is a browser companion → phone keeps
   notifying. This is the one-field change most likely to flip
   phone-notification behavior, and it matches the `messages-web-<hex>` naming
   already sent. **Make this first, alone, and test before anything else.**
2. **`OS: "libgm" → "Linux"`** (or `"Chrome OS"`). A non-standard OS string could
   bucket the device into a different class. Low cost; apply only if #1 alone
   doesn't move the roster `role`.
3. **`browserType: OTHER(1) → CHROME(2)`.** Same bucketing rationale; the real
   client is Chrome. Cosmetic-leaning; pair with #2.
4. **`RegisterRefresh` Web-Push registration** (`MoreParameters.PushReg{Type:
   "messages_web",…}`, `client.go`). Drives browser push delivery, not phone
   suppression — libgm can already register it and the phone still notifies.
   **Lowest rank; not the lever.**
5. **`SignInGaia` descriptor — RULED OUT.** Byte-identical between clients (§2.2).

```go
// pkg/libgm/util/config.go
var BrowserDetailsMessage = &gmproto.BrowserDetails{
    UserAgent:   UserAgent,
    BrowserType: gmproto.BrowserType_OTHER,   // step 3 (optional): → BrowserType_CHROME
    OS:          "libgm",                     // step 2 (optional): → "Linux"
    DeviceType:  gmproto.DeviceType_TABLET,   // STEP 1 (do first): → gmproto.DeviceType_WEB
}
```

---

## 4. WHAT TO CHANGE IN libgm AND HOW TO TEST IT

1. **Edit one field:** in `pkg/libgm/util/config.go`, change
   `DeviceType: gmproto.DeviceType_TABLET` → `gmproto.DeviceType_WEB`. No other
   code edits — `pair_google.go`/`pair.go` already reference
   `util.BrowserDetailsMessage`.
2. **Re-pair.** `BrowserDetails` is fixed at pair time and not refreshed, so the
   running openmessage/bridge session must re-pair (re-scan QR or re-run the Gaia
   pairing) for the new descriptor to take effect. A plain reconnect will NOT
   re-send it.
3. **Set the descriptor, reconnect, send yourself a text.** With the new device
   paired: from openmessage, send a message to your own number (a text you will
   see on the phone). Watch the phone.
4. **Check the phone.** If the phone **stops** getting a notification for that
   message (the paired "web" device now owns the thread), #1 is confirmed — the
   `deviceType` was the notification lever. If the phone still notifies, add
   step-2 (`OS`) and step-3 (`browserType`) and re-pair/re-test.
5. **Confirm at the protocol level (optional, decisive):** re-read the
   `SignInGaia` response `DeviceData` for the new device's regUUID (§2.2, f2
   `role` / f3 `deviceKind`). If the server-assigned `role` moves off `6` for the
   new device, the descriptor change propagated. If `role` is unchanged, the
   `deviceType` field alone isn't the bucketing input and steps 2–3 (or a caps
   field) are needed.

---

## 5. CORRECTION NOTE — the PullMessages / UseModernReceive scaffold is NOT the fix

`IMPLEMENTATION_NOTES.md` describes a `Client.UseModernReceive` flag,
`doPullMessages`/`pollReceive` split, and `PullMessagesURL` constants built on the
premise that "modern receive == PullMessages". **The capture disproves that
premise:**

- Modern receive is `ReceiveMessages` — the endpoint **libgm already uses** (§2.8).
- `PullMessages` returns only `[null,null,1]` heartbeats (§2.9); wiring it up
  changes nothing about message receipt.

Therefore:

- **Deprioritize / do not ship** the `UseModernReceive` receive-path scaffold as a
  fix for the notification problem. It solves a non-problem (libgm is already on
  the right receive verb) and its central unknown — the "PullMessagesRequest body
  below header=1 / resume cursor" flagged as the "#1 blocker" — is moot, because
  there is no message payload to pull.
- The PullMessages request layout is now known (`[header, []]`, §2.9) if a
  heartbeat/keep-alive is ever wanted, but it carries no messages.
- **Redirect effort to §3/§4:** the one-field `BrowserDetails.deviceType` change
  plus a re-pair. That is the actual notification lever the whole spec was chasing.

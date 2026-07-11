# 04 — Modern Google Messages web AUTH / REGISTRATION / ENCRYPTION path

Reverse-engineering the *modern* `messages.google.com/web` client's sign-in +
device-registration path, and how it differs from the older GAIA-pairing path
that `mautrix/gmessages` (`libgm`) implements. Leading question: **which
registration field plausibly tells Google "this is a companion, keep notifying
the phone"?**

Legend for confidence: **[OBSERVED]** = captured live this session (method
NAMES only, payloads were encrypted/blocked); **[SOURCE]** = read directly from
`libgm` source or a public proto; **[INFERRED]** = reasoning, not proven;
**[UNVERIFIED]** = hypothesis.

---

## 0. TL;DR on the notification question

- The registration schema the modern client and `libgm` use is **the same
  family** (Tachyon `instantmessaging-pa` fabric). The fields most likely to
  influence "keep notifying the phone" are, in order of plausibility:
  1. **`BrowserDetails.deviceType` / `BrowserType`** (device descriptor) —
     `libgm` deliberately mislabels itself `TABLET` + `OTHER` + `OS:"libgm"`;
     the real web client sends `WEB` + a real Chrome browser type. **[SOURCE]**
  2. **The receive *transport* chosen** — `ReceiveMessages` (persistent bind
     stream) vs `PullMessages` (catch-up pull). This is a behavioral, not a
     static-registration, difference. **[OBSERVED/INFERRED]**
  3. **The runtime foreground/"active device" assertion sent as a `SendMessage`
     on `hidden→visible`** — this is *presence*, not registration, and it
     correlates with notification suppression more tightly than any static
     registration field. **[OBSERVED]**
- **Honest caveat:** the phone's SMS/RCS notification-suppression decision lives
  in the Bugle/RCS layer, and none of the *encrypted* registration payloads were
  captured this session, so no single field is proven to be the switch. See §6.

---

## 1. Ground truth captured this session (method NAMES only)

Host: `https://instantmessaging-pa.googleapis.com/$rpc/…` (also the
`instantmessaging-pa.clients6.google.com` variant). **[OBSERVED]**

Modern web client RPCs seen:
- `…/v1.Messaging/SignInGaia`  *(see note below — `libgm` places SignInGaia
  under `…/v1.Registration/`, not `…/v1.Messaging/`)*
- `…/v1.Messaging/GetFiUserStanding`  *(Google Fi standing check; absent from libgm)*
- `…/v1.Messaging/ListIdentities`  *(enumerate the account's linked identities/devices)*
- `…/v1.Messaging/SendMessage`
- `…/v1.Messaging/PullMessages`  *(server-STREAMING receive; stays open minutes)*
- `…/v1.Messaging/AckMessages`
- `…/v1.Pairing/…` for pairing.

Behavioral observations **[OBSERVED]**:
- Receive = one long-lived streaming `PullMessages` (rarely re-issued).
- `hidden→visible`: client sends ONE `SendMessage` (an active/foreground
  assertion) + `AckMessages`.
- `visible→hidden`: client sends NOTHING.
- Message arrives while hidden: client sends `AckMessages` (sometimes +
  `SendMessage`), keeps it UNREAD, and **the phone still notifies**.

`libgm` (incl. upstream v0.2605.0) receives via `ReceiveMessages`, **not**
`PullMessages`; shares `SendMessage`+`AckMessages` names; has no
`GetFiUserStanding`/`ListIdentities`/`PullMessages`. **[OBSERVED/SOURCE]**

---

## 2. What `libgm` actually does today (authoritative for the schema)

`libgm` **already implements a GAIA sign-in path** (`SignInGaia`), not only the
QR relay pairing. Two registration/auth flows coexist:

| | QR / relay pairing | GAIA pairing (Gaia-linked) |
|---|---|---|
| network | `"Bugle"` | `"GDitto"` |
| entry RPC | `RegisterPhoneRelay` | `SignInGaia` then `DoGaiaPairing` (ukey2) |
| token refresh | `RegisterRefresh` | `RegisterRefresh` |
| source | `pkg/libgm/pair.go` | `pkg/libgm/pair_google.go` |

### 2.1 Endpoint map — `pkg/libgm/util/paths.go` **[SOURCE]**
```
instantMessagingBaseURL        = https://instantmessaging-pa.googleapis.com
instantMessagingBaseURLGoogle  = https://instantmessaging-pa.clients6.google.com

Pairing      = <base>/$rpc/google.internal.communications.instantmessaging.v1.Pairing
  /RegisterPhoneRelay /RefreshPhoneRelay /GetWebEncryptionKey /RevokeRelayPairing
Messaging    = <base>/$rpc/google.internal.communications.instantmessaging.v1.Messaging
  /ReceiveMessages /SendMessage /AckMessages           (also the …clients6… "Google" variant)
Registration = <clients6>/$rpc/google.internal.communications.instantmessaging.v1.Registration
  /SignInGaia /RegisterRefresh
```
> NOTE / discrepancy: `libgm` puts `SignInGaia`+`RegisterRefresh` on the
> **`…v1.Registration`** service (on the `clients6` host); the live capture this
> session logged `SignInGaia` grouped loosely under `…v1.Messaging`. Only NAMES
> were observable, so the service-prefix grouping in the capture is low
> confidence. The `Registration` service is the correct home per source. **[SOURCE]**

### 2.2 `SignInGaiaRequest` — `pkg/libgm/gmproto/authentication.proto` **[SOURCE]**
```proto
message SignInGaiaRequest {
  message Inner {
    message DeviceID {
      int32  unknownInt1 = 1;   // always 3
      string deviceID    = 2;   // "messages-web-{sessionID hex, no dashes}"
    }
    message Data { bytes someData = 3; } // PKIX-marshaled refresh public key
    DeviceID deviceID = 1;
    Data     someData = 36 [(pblite.pblite_binary) = true];
  }
  AuthMessage authMessage = 1;   // requestID, network="GDitto", configVersion
  Inner       inner       = 2;
  int32       unknownInt3 = 3;   // 1 on the initial call
  string      network     = 4;   // "GDitto"
}
```
Client builder — `pkg/libgm/pair_google.go:55` **[SOURCE]**:
```go
DeviceID: fmt.Sprintf("messages-web-%x", c.AuthData.SessionID[:])
```
Two-step: `signInGaiaInitial` (unknownInt3=1) then `signInGaiaGetToken` (attaches
the marshaled `RefreshKey` public key as `someData`), whose response yields the
Tachyon auth token and the `Device` descriptors (`Mobile`, `Browser`).

### 2.3 `SignInGaiaResponse` (excerpt) **[SOURCE]**
```proto
message SignInGaiaResponse {
  Header    header           = 1;
  string    maybeBrowserUUID = 2 [(pblite.pblite_binary) = true];
  DeviceData deviceData      = 3;   // deviceData.deviceWrapper.device -> Device
  TokenData tokenData        = 4;   // tachyonAuthToken + TTL
}
message Device { int64 userID = 1; string sourceID = 2; string network = 3; }
```
So the server assigns the browser a `Device{userID, sourceID, network}` +
`browserUUID`. `sourceID` is the durable per-browser device id.

### 2.4 The device DESCRIPTOR the client registers — `BrowserDetails` **[SOURCE]**
`pkg/libgm/gmproto/authentication.proto`:
```proto
enum DeviceType { UNKNOWN_DEVICE_TYPE = 0; WEB = 1; TABLET = 2; PWA = 3; }
message BrowserDetails {
  string      userAgent   = 1;
  BrowserType browserType = 2;   // CHROME/… ; libgm sends OTHER
  string      OS          = 3;
  DeviceType  deviceType  = 6;   // WEB(1) for real client; libgm sends TABLET(2)
}
```
`pkg/libgm/util/config.go` — **what `libgm` puts in the descriptor** **[SOURCE]**:
```go
var BrowserDetailsMessage = &gmproto.BrowserDetails{
    UserAgent:   UserAgent,                 // spoofed Chrome/Android UA
    BrowserType: gmproto.BrowserType_OTHER, // real client: a real browser type
    OS:          "libgm",                   // real client: "Windows"/"Linux"/…
    DeviceType:  gmproto.DeviceType_TABLET, // real client: WEB (or PWA)
}
```
`BrowserDetails` is only sent inside the GAIA-pairing containers
(`GaiaPairingRequestContainer.browserDetails`, `pair_google.go:499`) and the
relay `AuthenticationContainer.browserDetails` — **not** attached to
`RegisterRefresh`. There is **no explicit `primary` / `SMS-capable` /
`companion` boolean** anywhere in `libgm`'s schema. **[SOURCE]** The
companion-vs-primary distinction is implicit in `deviceType`/`browserType` +
`network` + which transport the device binds. **[INFERRED]**

### 2.5 `RegisterRefreshRequest` — token refresh + WEB PUSH registration **[SOURCE]**
```proto
message RegisterRefreshRequest {
  message PushRegistration {         // a W3C Web-Push subscription
    string type = 1; string url = 2; string p256dh = 3; string auth = 4;
  }
  message MoreParameters { int32 three = 1; PushRegistration pushReg = 102; }
  message Parameters { optional util.EmptyArr emptyArr = 9; optional MoreParameters moreParameters = 23; }
  AuthMessage messageAuth        = 1;
  Device      currBrowserDevice  = 2;   // the Device from SignInGaia
  int64       unixTimestamp      = 3;
  bytes       signature          = 4;   // ECDSA over "requestID:timestamp" w/ RefreshKey
  Parameters  parameters         = 13;
  int32       messageType        = 16;
}
```
`pkg/libgm/client.go:432 refreshAuthToken` sets the push registration when push
keys exist **[SOURCE]**:
```go
moreParams.PushReg = &gmproto.RegisterRefreshRequest_PushRegistration{
    Type: "messages_web", Url: keys.URL,
    P256Dh: b64(keys.P256DH), Auth: b64(keys.Auth),
}
```
i.e. even `libgm` can register a browser Web-Push endpoint (`type:"messages_web"`)
and then flips `SettingsUpdateRequest.PushSettings.Enabled = true`
(`client.go:413 RegisterPush`). So "has a web-push channel" is **not** by itself
what separates old vs new. **[SOURCE]**

### 2.6 Auth/token/cookie used by the receive path **[SOURCE]**
- **Tachyon auth token**: `AuthMessage.tachyonAuthToken` (bytes), obtained from
  `SignInGaia`→`TokenData` and refreshed by `RegisterRefresh`; carried in every
  Messaging RPC body (not a header). `client.go updateTachyonAuthToken`.
- **Google cookies + SAPISIDHASH**: `AuthData.AddCookiesToRequest`
  (`client.go:56`) attaches all saved cookies and sets
  `Authorization: SAPISIDHASH <ts>_<sha1(ts + " " + SAPISID + " " + origin)>`
  (`http.go:63 SAPISIDHash`, origin `https://messages.google.com`). GAIA path is
  cookie-bound; QR path is not.
- **API key header**: `x-goog-api-key: <GoogleAPIKey>` +
  `x-user-agent: grpc-web-javascript/0.1` (`util/func.go:16 BuildRelayHeaders`).
- Long-poll receive uses the same headers with a streaming body
  (`http.go` `longPoll=true`).

---

## 3. The Tachyon fabric the modern client rides on (public proto)

`instantmessaging-pa` is Google's generic **Tachyon** device-messaging fabric
(also used by Duo/Meet). The modern Messages web client's
`PullMessages`/`AckMessages`/registration verbs map onto Tachyon's
"Inbox/Bind" model. Best public reference:
`avaidyam/GoogleAPIProtobufs/…instantmessaging.v1.proto`
(https://github.com/avaidyam/GoogleAPIProtobufs). It uses `GTP…` names but the
same fabric. **[SOURCE]**

### 3.1 Registration carries an explicit CAPABILITIES vector + reg state **[SOURCE]**
```proto
message GTPRegisterData {              // avaidyam proto
  GTPDeviceId    deviceId    = 1;
  GTPDeviceInfo  deviceInfo  = 2;      // {os, hardware}
  string         iidToken    = 3;      // FCM/IID push token
  ...
  GTPPublicKey   identityKey = 6;
  string         locale      = 8;
  repeated int32 capsArray   = 9;      // <-- CAPABILITIES bitset/list
}
message GTPUserRegistrationState {     // broadcast about EACH device of the user
  GTPId          id_p     = 1;
  GTPRegistrationState_Type state = 2;
  repeated int32 capsArray = 3;        // per-device capabilities
}
message GTPDeviceId { GTPDeviceIdType_Type type = 1; string id_p = 2; }
message GTPId       { GTPIdType_Type type = 1; string id_p = 2; string app = 3; }
```
Other devices learn about a new registration via `GTPRegistrationChangePush` /
`GTPChangeProfilePush{ regState, caps }` (fields 5/6) and `ListIdentities`-style
enumeration (`GTPGetProfile…` → `GTPIdProfile{ regState, caps }`). **[SOURCE]**

### 3.2 Receive = a bind/pull stream **[SOURCE]**
```proto
message GTPBindRequest_Open { GTPRequestHeader header=1; GTPRegisterRefreshRequest registerRefreshRequest=2; GTPBindStream_Type streamType=3; }
message GTPBindRequest { Open open=4; Ping ping=5; Reload reload=6; Close close=7; Ack ack=8; }
message GTPInboxPullRequest  { int64 startTimestamp=1; GTPRequestHeader header=2; }
message GTPInboxPullResponse { repeated GTPInboxMessage messagesArray=1; ... }
```
This is exactly the `PullMessages` (catch-up) + long-lived bind (`ReceiveMessages`)
duality. `GTPRequestHeader` has `app`, `authTokenPayload`, `routingCookie`. **[SOURCE]**
The modern client's long-lived streaming `PullMessages` ≈ opening this bind
stream; `libgm`'s `ReceiveMessages` ≈ the same fabric, different verb/era. **[INFERRED]**

---

## 4. Old (libgm) vs modern web — side by side

| aspect | libgm (ReceiveMessages era) | modern web (PullMessages era) | conf |
|---|---|---|---|
| sign-in | `Registration/SignInGaia` (2-step) | `SignInGaia` (same family) | [SOURCE]/[OBSERVED] |
| Fi standing | — | `GetFiUserStanding` | [OBSERVED] |
| identity list | — | `ListIdentities` | [OBSERVED] |
| receive | `Messaging/ReceiveMessages` | `Messaging/PullMessages` (streaming) | [OBSERVED] |
| ack | `AckMessages` | `AckMessages` | [OBSERVED] |
| device descriptor | `BrowserDetails{TABLET, OTHER, OS:"libgm"}` | `BrowserDetails{WEB, real-browser, real-OS}` | [SOURCE]/[INFERRED] |
| token refresh + push | `RegisterRefresh` (+`messages_web` web-push) | `RegisterRefresh` (real web-push) | [SOURCE]/[INFERRED] |
| auth | Tachyon token in body + cookies + SAPISIDHASH | same | [SOURCE]/[INFERRED] |

---

## 5. Fields that could route "keep notifying the phone" (ranked)

1. **`BrowserDetails.deviceType` (WEB/TABLET/PWA) + `browserType`** — the clearest
   "what kind of companion am I" descriptor. `libgm` claims `TABLET`+`OTHER`; the
   real client claims `WEB`. Plausible that the primary/phone's notification-
   suppression heuristic keys off the companion's declared type. **[INFERRED, medium]**
2. **Tachyon `capsArray` (GTPRegisterData.capsArray / UserRegistrationState.caps)**
   — an explicit per-device capability vector broadcast to all of a user's
   devices; the natural place to encode "SMS-capable / can-render / companion".
   Not surfaced in `libgm`'s Messages-specific proto, so `libgm` may omit
   capability bits the modern client sets. **Strongest structural candidate.**
   **[INFERRED, medium]**
3. **`RegisterRefresh` web-push registration (`PushRegistration{url,p256dh,auth}`)**
   — whether the companion advertises a working browser push endpoint. But
   `libgm` also registers `type:"messages_web"` push, and the phone STILL
   notifies for the modern client, so this alone is not the switch. **[SOURCE, low]**
4. **Receive transport (bind `ReceiveMessages` vs catch-up `PullMessages`)** —
   holding a *persistent bind* stream may register the device as an actively
   "reachable" endpoint differently than periodic pulls. Behavioral, not a
   descriptor field. **[INFERRED, low–medium]**
5. **Runtime foreground assertion (`SendMessage` on `hidden→visible`)** — the
   observed data shows the client is silent when backgrounded and the phone keeps
   notifying; the active-device/presence assertion (a `SendMessage`, NOT a
   registration field) is what most tightly tracks suppression. Registration is
   likely *not* the lever here — **presence is.** **[OBSERVED, medium]**

---

## 6. Honest limits / next steps

- Encrypted `SignInGaia` / `RegisterRefresh` / `PullMessages` **payloads were NOT
  captured** this session — only method names. No field number in this doc for
  the *modern* wire format is proven; the schema quoted is `libgm`'s + the public
  Tachyon proto, which are the same *family* but may differ in exact field
  numbers/verbs from today's client. **Never treat the `libgm` field numbers as
  the modern client's.**
- The hypothesis "modern PullMessages/SignInGaia registration makes Google treat
  the device as a non-primary companion so the phone keeps notifying" is **not
  confirmed**. The observed evidence points at least as much to the **runtime
  foreground/presence `SendMessage`** as to any static registration field.
- To prove it: MITM the decrypted `RegisterRefresh`/`SignInGaia` bodies of the
  real client, diff `BrowserDetails.deviceType` + Tachyon `capsArray` +
  `PushRegistration` against `libgm`, then flip each in `libgm` and watch whether
  the phone's notification behavior changes. Compare a device on
  `ReceiveMessages` vs `PullMessages` with all descriptor fields held equal.

### Sources
- `pkg/libgm/gmproto/authentication.proto`, `pkg/libgm/util/{paths,config,func,constants}.go`,
  `pkg/libgm/{pair_google.go,pair.go,client.go,http.go}` (this repo).
- Tachyon fabric proto: https://github.com/avaidyam/GoogleAPIProtobufs/blob/master/google.internal.communications.instantmessaging.v1.proto
- Allo/Tachyon endpoint notes: https://github.com/avaidyam/Parrot/wiki/Allo-Endpoints

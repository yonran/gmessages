# CAP-02 — JSPB protobuf definitions for instantmessaging.v1 (Messaging / Registration / MessagesMultiDevice)

Recovered message + enum definitions for the modern Tachyon-based Google Messages
web API. Everything below is REDACTED: real request IDs, tokens, phone numbers,
emails, account IDs and key material are replaced with typed placeholders
`<type:meaning>`. Only STRUCTURE (JSPB array index → proto field number → type →
meaning) is documented.

## Sources & method

- Captured JS bundles: `/Users/yonran/third-party/gmcap/scripts/*.js`
  (closure-compiled, `mw_*.js` + `sy*.js` + `e2QYhf-27.js`).
- Captured live JSPB bodies: `/Users/yonran/third-party/gmcap/rpc/<Method>-<id>.network-{request,response}`.
- Cross-reference: `/Users/yonran/repos/gmessages/pkg/libgm/gmproto/authentication.proto`
  (+`.pb.go`), `pair_google.go`, `util/paths.go`.

**Key finding about the JS bundles.** Closure compilation stripped every proto
message/enum NAME and reflection descriptor. Grepping the bundles for
`SignInGaia`, `BrowserDetails`, `DeviceType`, `UNKNOWN_DEVICE_TYPE`,
`capabilit*`, or a literal `"GDitto"` returns **nothing** — those are either
runtime-config strings or renamed locals. What DID survive:

- The RPC package prefix is literal for a few services, confirming the naming
  scheme for all of them:
  `google.internal.communications.instantmessaging.v1.Group/GetGroupInfo`,
  `.../Group/CreateGroup`, `.../Abuse/GetURLState`,
  `sticker.v1.StickerService/{ListStickerPacks,SearchStickers}`
  (in `mw_rcs_chat-36.js`).
- The device-descriptor id construction (`mw_rcs_chat-36.js`):
  ```js
  pRc=async function(a){return _.MSa(_.LSa(),"messages-web-"+(a.fS()||_.sm()))};
  ```
  i.e. deviceID = `"messages-web-" + <hex session id>` — matches libgm
  `pair_google.go` `fmt.Sprintf("messages-web-%x", c.AuthData.SessionID[:])`.

So the message/enum BODIES below are reconstructed from the live JSPB captures
and named via libgm gmproto. Confidence is per-section.

## Service / method map  (confidence: HIGH)

All under `https://instantmessaging-pa{,.clients6}.googleapis.com/$rpc/google.internal.communications.instantmessaging.v1.<Service>/<Method>`
(prefix confirmed literal in JS; paths confirmed in libgm `util/paths.go`).

| Service | Method | Host variant | Captured file |
|---|---|---|---|
| Messaging | ReceiveMessages | instantmessaging-pa | ReceiveMessages-56 (~103 KB streamed msgs) |
| Messaging | SendMessage | instantmessaging-pa | SendMessage-50/51/89/115 |
| Messaging | AckMessages | instantmessaging-pa | AckMessages-82/83/110 |
| Messaging | PullMessages | …-jms-us / clients6 | PullMessages-80/85/113 (heartbeat only) |
| Registration | SignInGaia | clients6 | SignInGaia-46/47 |
| Registration | ListIdentities | clients6 | ListIdentities-49 |
| Registration | LookupRegistered | clients6 | LookupRegistered-87 |
| MessagesMultiDevice | GetFiUserStanding | clients6 | GetFiUserStanding-48 |

Receive-path note (confirmed by capture): the modern web client's real inbound
message data arrives on **ReceiveMessages** (same endpoint libgm already uses).
`PullMessages` returns only `[null,null,1]` heartbeats. The phone-notification
routing difference is therefore in the **registration** calls, not receive.

---

## RequestHeader  (= libgm `AuthMessage`)  (confidence: HIGH)

The first element of every request body. Proto `AuthMessage`
(authentication.proto): fields 1/3/6/7.

```
[ <string:requestId(uuidv4)>,   // idx0 = field1  requestID
  null,                          // idx1 = field2  (unused)
  <string:network>,              // idx2 = field3  network: "GDitto"|"RCS"|"CMS"
  null,                          // idx3 = field4
  null,                          // idx4 = field5
  <bytes:tachyonAuthToken|null>, // idx5 = field6  tachyon auth blob (base64)
  <ConfigVersion> ]              // idx6 = field7  configVersion (see below)
```

- `field6` tachyonAuthToken is present ONLY on token-authed calls
  (PullMessages, LookupRegistered). It is `null` on cookie/GAIA-authed calls
  (SignInGaia, ListIdentities, GetFiUserStanding).
- `network` selects the backend routing: `"RCS"` (messaging), `"GDitto"`
  (Google-account registration/device), `"CMS"` (GetFiUserStanding).

### ConfigVersion  (field7)  (confidence: HIGH)

Proto `ConfigVersion`: `Year=3, Month=4, Day=5, V1=7, V2=9`.
Captured `[null,null,2026,7,8,null,4,null,6]`:

```
idx2=field3 Year  = <int:2026>
idx3=field4 Month = <int:7>
idx4=field5 Day   = <int:8>
idx6=field7 V1    = <int:4>
idx8=field9 V2    = <int:6>
```

## ResponseHeader  (confidence: HIGH)

First element of most responses (`SignInGaiaResponse.Header`,
`LookupRegistered` header). Fields 2 and 4:

```
[ null,
  <uint64:responseId>,   // idx1 = field2
  null,
  <int64:tsMicros> ]     // idx3 = field4  server timestamp (µs)
```
Some responses carry only `[null,<uint64:responseId>]` (ListIdentities,
GetFiUserStanding).

---

## ContactId / Identity  (confidence: HIGH — from captures)

A recurring 3-tuple used as request selector and in ListIdentities results.
`[ <int:idType>, <string:value>, <string:network> ]`.

`idType` enum (observed):

| value | meaning | evidence |
|---|---|---|
| 1 | phone number (E.164) | `[1,"<phone:e164>","RCS"]` in ListIdentities resp, LookupRegistered req |
| 16 | GAIA / email | `[16,"<string:email>","RCS"]` and `[16,"<string:email>","GDitto"]` in SignInGaia resp / ListIdentities req |

---

## BrowserDetails + enums  (confidence: HIGH for enum values — from libgm authentication.proto; not observed in these captures)

The device-registration descriptor. Not present in the captured Registration
bodies (SignInGaia uses the compact `Device` descriptor, below), but it is the
canonical descriptor for the pairing/relay path and defines the DeviceType enum.

```
message BrowserDetails {          // proto field numbers
  string userAgent    = 1;
  BrowserType browserType = 2;
  string OS           = 3;
  DeviceType deviceType = 6;
}
```

### enum BrowserType

```
UNKNOWN_BROWSER_TYPE = 0
OTHER   = 1
CHROME  = 2
FIREFOX = 3
SAFARI  = 4
OPERA   = 5
IE      = 6
EDGE    = 7
```

### enum DeviceType  (the small/registration enum)

```
UNKNOWN_DEVICE_TYPE = 0
WEB    = 1
TABLET = 2
PWA    = 3
```

Note: this is the *pairing-descriptor* DeviceType. The per-device role/kind
integers seen in the SignInGaia response (values 1/6/19, below) are a
**different, larger** enum domain and are NOT this DeviceType.

---

## SignInGaia — Registration service  (confidence: HIGH)

### Request  (proto `SignInGaiaRequest`)

Captured `SignInGaia-46.network-request`:
```
[ <RequestHeader>,                                // idx0 = field1 authMessage (network="GDitto", token=null)
  [ [ 3, "messages-web-<hex:sessionId>" ] ],      // idx1 = field2 inner
  1,                                              // idx2 = field3 unknownInt3 (const 1)
  "GDitto" ]                                       // idx3 = field4 network
```
Inner (`SignInGaiaRequest.Inner`):
- `field1 DeviceID = [ <int:3>, <string:deviceId> ]`
  - `idx0=field1` = `3` (constant device-registration discriminator; libgm comments it as `3`)
  - `idx1=field2` = `"messages-web-" + <hex sessionId>` (32 hex chars = 16-byte session id)
- `field36 someData` (pblite binary, present only on the *get-token* second call;
  absent in the initial call) — an encryption key blob.

### Response  (proto `SignInGaiaResponse`)  (confidence: HIGH)

Captured `SignInGaia-46.network-response` (redacted):
```
[ <ResponseHeader>,             // idx0 field1: [null,<uint64:respId>,null,<int64:ts>]
  null,                          // idx1 field2  maybeBrowserUUID (pblite binary)
  [ <DeviceData> ] ]             // idx2 field3  deviceData
```

`DeviceData` (field3) — this is the **account device roster** that governs
phone-notification routing:
```
[
  [ [ 16, "<string:email>", "GDitto" ] ],   // field1 deviceWrapper → Device (idType=16 gaia)
  [ <Item1>, <Item1>, ... ],                // field2 per-device summary rows
  [ <Item4>, <Item4>, ... ],                // field3 per-device detail rows
  [ null, [ [], [] ] ]                       // field4 (no observed data)
]
```

**field2 rows** (`RPCGaiaData…Item2.Item1`), one per registered device:
```
[ <string:deviceUUID>, null, null,          // idx0=field1 UUID (pblite b64)
  <int:role>,                                // idx3=field4  role/kind (1 or 6)
  <string:lang|null>,                        // idx4=field5  languageCode ("en-US" only on local)
  null,
  <uint64:deviceBigId> ]                     // idx6=field7
```

**field3 rows** (`RPCGaiaData…Item4`), one per registered device:
```
[ <string:deviceUUID>, null,                // idx0=field1 UUID
  <int:localFlag>,                           // idx2=field3  1=this/local device, 6=remote paired device
  <int:deviceKind>,                          // idx3=field4  device kind enum (6 or 19 observed)
  null, null,
  <int64:createTsMicros>,                    // idx6=field7  device creation ts (µs)
  <bytes:item8> ]                            // idx7=field8  per-device blob (pblite b64)
```
- `item8` for the LOCAL device (localFlag=1) is a small protobuf-ish blob
  (`CAEQ…IAE=`); for one remote device it is an ECDSA/prime256v1 public key
  (ASN.1 `…CE…prime256v1…` visible in the b64), for others a 32-byte value.

### Observed device roster (4 devices, redacted) — enum evidence

| device | field2.role (f4) | field3.localFlag (f3) | field3.deviceKind (f4) |
|---|---|---|---|
| local web (has lang en-US, item8 CAEQ…) | 1 | 1 | 19 |
| remote (has EC pubkey item8)            | 6 | 6 | 6 |
| remote                                   | 6 | 6 | 6 |
| remote (item8 32-byte)                   | 6 | 6 | 19 |

---

## Device / role enums observed in SignInGaia response  (confidence: MEDIUM)

Two distinct integer enums appear on the account device roster. Values are
observed; symbolic NAMES are NOT recoverable (stripped from JS, and libgm marks
them `unknownInt*`).

- **localFlag** (Item4 field3, Item1 also uses field4 with same domain):
  - `1` = the local/this web device
  - `6` = a remote paired device (phone / other client)
- **deviceKind** (Item4 field4):
  - `6` and `19` observed. libgm's comment guessed "always 6"; **correction:
    `19` also occurs** (on the local web device and one remote device). Meaning
    of the 6-vs-19 split is not yet determined from this capture.

These are the "type enums (seen: 1, 6, 19)" referenced in the task: the domain
across both roster columns is `{1, 6, 19}` — `1`=local, `6`=remote/other,
`19`=an alternate kind (co-occurs with both local and remote rows).

---

## ListIdentities — Registration service  (confidence: HIGH)

Request `ListIdentities-49`:
```
[ <RequestHeader(network="RCS")>,   // idx0 field1
  [ 16, "<string:email>", "RCS" ] ] // idx1 field2  ContactId selector (idType=16 gaia)
```
Response:
```
[ [ null, <uint64:respId> ],                 // idx0 header
  [ [ 1, "<phone:e164>", "RCS" ] ] ]          // idx1 field2  list of resolved identities (idType=1 phone)
```
i.e. resolves the account's GAIA identity to its registered RCS phone identity.

## LookupRegistered — Registration service  (confidence: HIGH)

Request `LookupRegistered-87` (token-authed, network="RCS"):
```
[ <RequestHeader(token set)>,             // idx0 field1
  [ [ 1, "<phone:e164>", "RCS" ] ],        // idx1 field2  target(s) to look up (idType=1 phone)
  null,null,null,null,
  1 ]                                       // idx6 field7  flag (const 1)
```
Response:
```
[ <ResponseHeader>,                        // idx0 field1
  null,
  [ [ 1, "<phone:e164>", "RCS" ] ],         // idx2 field3  echoed target ContactId
  [ [ [ 1,"<phone:e164>","RCS" ],           // idx3 field4  registration record
      4,                                     //   status/int
      null,null,
      [ [ null,…,[ <RCS feature/IARI tags> ] ] ] ] ] ]  // RCS capability/feature tags
]
```
The nested tags are 3GPP RCS capability strings (`+g.3gpp.iari-ref`,
`urn:urn-7:3gpp-application.ims.iari.rcse.im`, `…rcs.fthttp`,
`+g.gsma.rcs.msgfallback`, …) — i.e. the target's RCS capability advertisement.
This is the closest thing to a "Capability" message; it is a repeated list of
`[[featureKey],[featureValue]]` tag pairs, not an enum.

## GetFiUserStanding — MessagesMultiDevice service  (confidence: HIGH)

Request `GetFiUserStanding-48`:
```
[ <RequestHeader(network="CMS", token=null)> ]   // header only
```
Response:
```
[ [ null, <uint64:respId> ] ]                     // header only; no standing payload for this account
```

---

## PullMessages — Messaging service  (confidence: HIGH)

Request `PullMessages-80` (token-authed, network="RCS"):
```
[ <RequestHeader(token set)>, [] ]   // idx0 header, idx1 = empty request body {}
```
Response: `[null,null,1]` — a heartbeat (`field3=1`), no message payload.
Confirms PullMessages is a keep-alive long-poll, not the real receive path.

---

## Confidence summary

| Item | Confidence | Basis |
|---|---|---|
| Service/method paths + RPC package prefix | HIGH | literal in JS (Group/Abuse/sticker) + libgm paths.go + captures |
| RequestHeader/AuthMessage field map | HIGH | libgm authentication.proto + all captures agree |
| ConfigVersion field map | HIGH | libgm + capture |
| ResponseHeader | HIGH | captures |
| ContactId idType (1=phone,16=gaia) | HIGH | captures |
| BrowserType / DeviceType(WEB/TABLET/PWA) enum values | HIGH | libgm authentication.proto (values), NOT observed in these captures |
| SignInGaia request/response structure | HIGH | libgm gmproto + capture |
| SignInGaia roster role enum (1/6) | MEDIUM | inferred from 4-device sample |
| SignInGaia deviceKind enum (6/19) | MEDIUM | observed; meaning of split unknown; corrects libgm "always 6" |
| ListIdentities / LookupRegistered / GetFiUserStanding / PullMessages bodies | HIGH | captures |
| JSPB proto class field arrays from bundles | N/A | names stripped by closure; not recoverable |

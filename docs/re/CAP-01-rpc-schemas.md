# CAP-01 — Field-mapped schemas for captured modern-web RPC bodies

Source captures: `/Users/yonran/third-party/gmcap/rpc/<Method>-<reqid>.network-{request,response}`.
libgm reference protos: `/Users/yonran/repos/gmessages/pkg/libgm/gmproto/{authentication,client,rpc}.proto`.

Convention: these bodies are **JSPB** (positional JSON arrays, `null` = absent). Every proto
field number below = **JSPB array index + 1**. Wire type is inferred from the observed value.

**REDACTION:** all real tokens / phone numbers / account ids / message ids / base64 blobs are
replaced with typed placeholders `<type:name>`. Only structure is documented.

---

## 0. RequestHeader == `authentication.AuthMessage` (shared by every registration/RCS/GDitto call)

The first array element of almost every request is the header. Observed shape:

```
[ <string:requestId>, null, <string:network>, null, null, <string:tachyonAuthToken>, <ConfigVersion> ]
```

```proto
message RequestHeader {          // identical to authentication.AuthMessage
  string requestId        = 1;   // UUID, idx0
  // field 2               = 2;   // idx1, always null in captures
  string network          = 3;   // idx2: "GDitto" | "RCS" | "CMS"
  // field 4/5             = 4/5; // idx3,idx4 always null
  bytes  tachyonAuthToken = 6;   // idx5: base64 tachyon auth blob  <-- TOKEN LIVES AT FIELD 6
  ConfigVersion configVersion = 7; // idx6
}
```

**Tachyon auth token carriage:** field **6** (JSPB index 5), a base64 string. In the pre-registration
`SignInGaia` and `GetFiUserStanding` calls this field is `null` (unauthenticated); once registered
every `RCS`/`GDitto` call carries it. This matches libgm `AuthMessage` exactly
(`requestID=1, network=3, tachyonAuthToken=6, configVersion=7`). **No divergence.**

`ConfigVersion` (idx6 = field 7), observed `[null,null,2026,7,8,null,4,null,6]`:

```proto
message ConfigVersion { int32 Year=3; int32 Month=4; int32 Day=5; int32 V1=7; int32 V2=9; }
```
Byte-for-byte identical to libgm `authentication.ConfigVersion`.

---

## 1. SignInGaia  (network "GDitto", device registration)

### Request `[SignInGaia-46]`
```
[ RequestHeader(token=null), [[3,"messages-web-<hex32>"]], 1, "GDitto" ]
```
```proto
message SignInGaiaRequest {
  RequestHeader authMessage = 1;   // idx0, token null (pre-reg)
  message Inner {                  // idx1
    message DeviceID { int32 unknownInt1 = 1; string deviceID = 2; } // [3, "messages-web-<hex32>"]
    DeviceID deviceID = 1;
  }
  Inner   inner       = 2;
  int32   unknownInt3 = 3;         // idx2 = 1
  string  network     = 4;         // idx3 = "GDitto"
}
```
**Matches** libgm `authentication.SignInGaiaRequest` (the optional `Inner.someData` field 36 is
absent in this capture). Device is registered as **type 3**, name `messages-web-<hex32>`.

### Response `[SignInGaia-46]`
```
[ [null,<uint64>,null,<int64:ts>], null, <DeviceData> ]
```
```proto
message SignInGaiaResponse {
  message Header { uint64 unknownInt2 = 2; int64 unknownTimestamp = 4; }
  Header header           = 1;   // idx0
  string maybeBrowserUUID = 2;   // idx1 = null
  DeviceData deviceData   = 3;   // idx2  (tokenData field 4 absent)
}
```
`DeviceData` (idx2) = 4-element array:
```
[ [[16,<string:acct>,"GDitto"]],          // f1 deviceWrapper -> Device{userID=16, sourceID=acct, network}
  [ [<b64:id>,null,null,1,"en-US",null,<uint64>], [<b64:id>,null,null,6,null,null,<uint64>], ... ], // f2 identities (repeated)
  [ [<b64:id>,null,1,19,null,null,<int64:ts>,"<b64:CAEQ...>"], ... ],  // f3 devices (repeated)
  [ null, [[],[]] ] ]              // f4 unknown/empty
```
**This is the payload that matters for phone-notification routing:** f3 enumerates *every device on
the account* with a **device-type enum** (observed values **1, 6, 19** at the 3rd/4th positions).
Matches libgm `SignInGaiaResponse.DeviceData` (libgm labels f2/f3 as `unknownItems2/3`).

---

## 2. GetFiUserStanding  (network "CMS")

### Request `[GetFiUserStanding-48]`  `[ RequestHeader(token=null,"CMS") ]`
```proto
message GetFiUserStandingRequest { RequestHeader header = 1; }   // header only
```
### Response `[[null,<uint64>]]`
```proto
message GetFiUserStandingResponse { message H { uint64 id = 2; } H result = 1; }
```
**No libgm equivalent** — new method. Trivial; header-only request, opaque id response.

---

## 3. ListIdentities  (network "RCS")

### Request `[ListIdentities-49]`
```
[ RequestHeader(token=null,"RCS"), [16,<string:acct>,"RCS"] ]
```
```proto
message ListIdentitiesRequest {
  RequestHeader header = 1;                 // idx0
  authentication.Device identity = 2;       // idx1: [16, acct, "RCS"] = Device{userID=16,sourceID=acct,network}
}
```
### Response `[ [null,<uint64>], [[1,<string:e164>,"RCS"]] ]`
```proto
message ListIdentitiesResponse {
  message H { uint64 id = 2; } H header = 1;             // idx0
  repeated authentication.Device identities = 2;         // idx1: [[1, "+<e164>", "RCS"]] -> registered RCS phone
}
```
**No libgm equivalent.** Reuses `authentication.Device` shape. Returns the account's registered
E.164 RCS number.

---

## 4. LookupRegistered  (network "RCS")

### Request `[LookupRegistered-87]`
```
[ RequestHeader(token,"RCS"), [[1,<string:e164>,"RCS"]], null,null,null,null, 1 ]
```
```proto
message LookupRegisteredRequest {
  RequestHeader header = 1;                       // idx0 (token PRESENT)
  repeated authentication.Device lookup = 2;      // idx1: numbers to look up
  // idx2..5 null
  int32 unknownFlag = 7;                          // idx6 = 1
}
```
### Response `[LookupRegistered-87]`
```
[ [null,<uint64>,null,<int64:ts>], null,
  [[1,<string:e164>,"RCS"]],                       // f3: echoed queried numbers
  [ [ [1,<string:e164>,"RCS"], 4, null,null, <RcsCapabilities> ] ] ]  // f4: per-number result
```
```proto
message LookupRegisteredResponse {
  message H { uint64 id=2; int64 timestamp=4; } H header = 1;   // idx0
  // idx1 null
  repeated authentication.Device queried = 3;                    // idx2
  message Result {
    authentication.Device device = 1;
    int32 status = 2;              // observed 4
    // 3,4 null
    RcsFeatureTags capabilities = 5; // 3GPP IARI/ICSI feature-tag tree (rcse.im, fthttp, cpm.session.group, msgfallback)
  }
  repeated Result results = 4;                                   // idx3
}
```
**No libgm equivalent.** Confirms RCS reachability + capabilities of a target number.

---

## 5. SendMessage  (network "GDitto")  ==  libgm `rpc.OutgoingRPCMessage`

### Request `[SendMessage-50]`
```
[ [16,<string:acct>,"GDitto"],                          // f1 mobile (Device)
  [ <string:tmpID>,19, null×9, "<b64:OutgoingRPCData>", null×10, [null,2] ], // f2 data
  RequestHeader(token,"GDitto"),                          // f3 auth (AuthMessage)
  null, 86400000000, null,null,null,                      // f4, f5 TTL, f6-8
  ["<b64:destRegistrationId>"] ]                          // f9 destRegistrationIDs
```
```proto
message SendMessageRequest {          // == rpc.OutgoingRPCMessage
  authentication.Device mobile = 1;   // idx0
  message Data {
    string requestID   = 1;           // idx0 of f2 = tmpID
    BugleRoute bugleRoute = 2;         // idx1 = 19 (DataEvent)
    bytes  messageData = 12;           // idx11 = base64 OutgoingRPCData
    message Type { util.EmptyArr emptyArr = 1; MessageType messageType = 2; }
    Type   messageTypeData = 23;       // idx22 = [null,2]
  }
  Data   data = 2;                     // idx1
  AuthMessage auth = 3;                // idx2  (== RequestHeader)
  int64  TTL = 5;                      // idx4 = 86400000000
  repeated string destRegistrationIDs = 9; // idx8, pblite_binary base64
}
```
**Matches libgm `rpc.OutgoingRPCMessage` field-for-field** (mobile=1, data=2{reqID=1,route=2,
messageData=12,typeData=23}, auth=3, TTL=5, destRegistrationIDs=9).

### Response `[[null,<uint64>], <string:ts>]`
```proto
message SendMessageResponse {           // == rpc.OutgoingRPCResponse
  message SomeIdentifier { string someNumber = 2; } SomeIdentifier someIdentifier = 1;
  optional string timestamp = 2;
}
```
Matches libgm `rpc.OutgoingRPCResponse`.

---

## 6. ReceiveMessages  (network "GDitto" and "RCS")  —  THE MODERN INBOUND PATH

### Request `[ReceiveMessages-56]`  `[ RequestHeader(token,"GDitto"), null, null, [] ]`
```proto
message ReceiveMessagesRequest {
  authentication.AuthMessage auth = 1;   // idx0
  // idx1,idx2 null
  UnknownEmptyObject unknown = 4;         // idx3 = []  (empty)
}
```
**Structural match with libgm `client.ReceiveMessagesRequest`:** libgm places `auth=1` and an
optional empty object at `unknown=4`; the capture is `[auth, null, null, []]` — auth at field 1,
empty at field 4. The only nuance: libgm models field 4 as a nested `UnknownEmptyObject2{unknown=2:{}}`
(`[null,[]]`), while the capture sends a bare empty array `[]`. Same field numbers, same auth
placement — **effectively byte-for-byte in structure.** This confirms the modern receive path is the
SAME `ReceiveMessages` endpoint libgm already implements.

### Response `[ReceiveMessages-56]` (~103 KB stream)
Top level = `[ [ entry, entry, ... ] ]`. Each `entry` is a `LongPollingPayload`-shaped 2/3-elem array:

- Data entry: `[ null, <IncomingRPCMessage> ]` — field 2 = data.
- Heartbeat/ack entry: `[ null, null, [] ]` — field 3 = empty (most entries in RM-56/RM-81).
- Terminal (RM-81): `[ 1, "The operation was cancelled." ]` — stream close.

`IncomingRPCMessage` (the inner array of a data entry):
```
[ <string:responseID>,19,<uint64:startExec>,null,3,<uint64:finishExec>,"<string:usTaken>",
  [16,<acct>,"GDitto"], [16,<acct>,"GDitto"], null,null, "<b64:messageData>",
  null,null,null,null, "<b64:signatureID>" ]
```
```proto
message IncomingRPCMessage {            // == rpc.IncomingRPCMessage
  string responseID = 1;                // idx0
  BugleRoute bugleRoute = 2;            // idx1 = 19
  uint64 startExecute = 3;             // idx2
  MessageType messageType = 5;         // idx4 = 3
  uint64 finishExecute = 6;            // idx5
  uint64 microsecondsTaken = 7;        // idx6
  authentication.Device mobile = 8;    // idx7
  authentication.Device browser = 9;   // idx8
  bytes messageData = 12;              // idx11 (encrypted OutgoingRPCData/RPCMessageData)
  string signatureID = 17;             // idx16
}
```
**Matches libgm `rpc.IncomingRPCMessage` field-for-field.** Confirms inbound message data arrives
here (not via PullMessages).

---

## 7. PullMessages  (network "RCS", the `-jms-us` host)  —  heartbeat only

### Request `[PullMessages-80]`  `[ RequestHeader(token,"RCS"), [] ]`
```proto
message PullMessagesRequest {
  RequestHeader header = 1;   // idx0
  util.EmptyArr cursor = 2;   // idx1 = []  (no resume cursor observed)
}
```
This finally pins the layout libgm left as a TODO in `client.proto` (header=1; the second element is
an empty array, not an encrypted cursor). 

### Response `[null,null,1]`
```proto
message PullMessagesResponse { /* idx2 = field 3 = 1 */ }
```
Every observed response is `[null,null,1]` — a **HEARTBEAT / no-op** (field 3 = 1), carrying zero
message data. Shape aligns with `rpc.LongPollingPayload` (heartbeat=3). **Confirms: implementing
PullMessages would NOT change message receipt** — real messages come over ReceiveMessages (§6).

---

## 8. AckMessages  (network "GDitto")  —  **DIVERGES from libgm**

### Request `[AckMessages-82]`
```
[ RequestHeader(token,"GDitto"), [ <string:msgId>, <string:msgId>, ... ] ]
```
```proto
message AckMessagesRequest {
  RequestHeader header = 1;             // idx0
  repeated string messageIds = 2;       // idx1: FLAT list of message-request UUIDs
}
```
### Response `[[null,<uint64>]]`  — `SomeIdentifier{someNumber=2}` echo.

**KEY DIVERGENCE from libgm `client.AckMessageRequest`:** libgm models it as
`{ authData=1, util.EmptyArr emptyArr=2, repeated Message acks=4 }` where each `Message =
{requestID=1, Device device=2}`. The **modern capture is flatter**: the acked IDs are a bare
`repeated string` at **field 2** (no `emptyArr`, no `Device` wrapper, no field 4). A libgm-shaped
ack body would put the IDs in the wrong field for the modern endpoint.

---

## Summary of match vs. divergence

| Method | vs libgm | Notes |
|---|---|---|
| RequestHeader/AuthMessage | **match** | token at field 6 (idx5), base64 |
| SignInGaia req/resp | **match** | device type 3; resp f3 lists devices w/ type enums 1,6,19 |
| GetFiUserStanding | new | header-only, opaque id |
| ListIdentities | new | Device arg; returns registered E.164 |
| LookupRegistered | new | RCS reachability + 3GPP capabilities |
| SendMessage | **match** | == OutgoingRPCMessage exactly |
| ReceiveMessages req | **match (structural)** | auth=1, empty@4; modern inbound path |
| ReceiveMessages resp | **match** | == IncomingRPCMessage exactly |
| PullMessages | pins TODO | header=1, empty@2; resp = heartbeat `[null,null,1]` only |
| AckMessages | **DIVERGES** | flat `repeated string` @ field 2 vs libgm Message-wrapped acks @ field 4 |

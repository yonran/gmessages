# Google Messages web client — instantmessaging v1 wire schema (JS reverse-engineering)

Reverse-engineered from the live `messages.google.com/web` closure-compiled JSPB bundle.

## Provenance

- Page: `https://messages.google.com/web` → references one app bundle:
  `https://www.gstatic.com/_/messagesweb/_/js/k=messagesweb.mw.en_US.droovuxNHkg.O/am=hIAAZwAQAAAB/d=1/rs=AIp04d8-XMwgCjlAyOlSIt2OJydBCfdbMA/m=mw_b`
  (public, no auth; 1,325,778 bytes; saved as `/tmp/gm_mw_b.js`).
- All snippets below are quoted verbatim from that file.

## How the schema is encoded (method)

RPC descriptors are `new _.OD(path, mode, ReqType, RespType, serializeFn, deserializeFn)`:

```
var l6a=new _.OD("/google.internal.communications.instantmessaging.v1.Messaging/PullMessages","unary",tTa,k6a,a=>a.serialize(),_.wd(k6a));
var m6a=new _.OD("/google.internal.communications.instantmessaging.v1.Messaging/ReceiveMessages","server_streaming",uTa,_.Jz,a=>a.serialize(),zTa);
var o6a=new _.OD("/google.internal.communications.instantmessaging.v1.Messaging/SendMessage","unary",_.QD,_.iD,a=>a.serialize(),b4a);
```

Messages extend `_.n` (JSPB Message). This runtime is **schema-less on decode**: `_.wd(k6a)=b=>_.yda(k6a,b)` decodes bytes into a sparse internal array keyed by field number, and **field types are carried by the getter/setter call sites**, not a central table:

- `_.T(this,Type,N)` / `_.U(this,N,v)` / `_.rq(this,Type,N)` → singular **message** field `N` of type `Type` (HIGH).
- `_.q(this,N)` → **string** field `N` — `_.q=function(a,b){return(c=_.Wb(_.xn(a,b)))!=null?c:""}`, `_.Wb` checks `typeof==="string"` (HIGH).
- `_.tn(this,N)` → **int32/enum** field `N` — `_.tn=function(a,b,c=0){return(_.Dq(a,b))!=null?...:c}` (HIGH type-class).
- `_.un(this,N)` → **bytes** field `N` — default `_.cb()` = `new _.db(null)` ByteString (HIGH type-class).
- `_.Vo(this,N)` → **double/number** field `N` — `_.Vo=function(a,b,c=Bya){return(_.xn(a,b,...,_.Rb))!=null?...:c}` (MED).

**Consequence:** for the six target RPCs the request/response *wrappers* are thin (`header` + a payload sub-message), and the payload sub-message bodies the web client actually reads are minimal — the bulk of each message body is **encrypted opaque bytes** that the client never introspects, so no named getters exist for them. This matches the ground-truth observation that payloads were encrypted. Field numbers below are only those the client code explicitly touches; everything else is genuinely `UNKNOWN` from static analysis.

Descriptor arrays (full field maps) exist only for a handful of types, bound via `_.td(Class,[array])`, e.g. `_.td(_.c4a,[0,_.Gz,_.Er])`. The shared header sub-schema is `_.Gz` (see bottom).

## RPC catalog (HIGH confidence — verbatim descriptors)

`instantmessaging-pa.googleapis.com/$rpc/google.internal.communications.instantmessaging.v1.<Service>/<Method>`

| Service | Method | Mode | Req | Resp |
|---|---|---|---|---|
| Messaging | SendMessage | unary | `_.QD` | `_.iD` |
| Messaging | PullMessages | **unary** | `tTa` | `k6a` |
| Messaging | AckMessages | unary | `iTa` | `_.$3a` |
| Messaging | ReceiveMessages | **server_streaming** | `uTa` | `_.Jz` |
| Messaging | PrewarmReceiver | unary | `h6a` | `i6a` |
| Messaging | Echo | unary | `kTa` | `f6a` |
| Registration | SignInGaia | unary | `u7a` | `_.k4a` |
| Registration | SignInSecondary | unary | `_.w7a` | `_.oE` |
| Registration | ListIdentities | unary | `_.k7a` | `_.l7a` |
| Registration | Register / RegisterRefresh / LinkIdentity / LookupRegistered / Unregister / DeleteAccount / GetAccountInfo | unary | — | — |
| MessagesMultiDevice | GetFiUserStanding | unary | `_.Jab` | `_.dD` |
| MessagesMultiDevice | SendFiMessage / MdmList* | unary | — | — |
| Pairing | RegisterPhoneRelay / RefreshPhoneRelay / RevokeRelayPairing / GetWebEncryptionKey | unary | — | — |
| SmartMessaging | GetContentDecoration / CreateConversation | unary | — | — |

### PullMessages transport — reconciliation with ground truth (HIGH)

The bundle declares **PullMessages as `"unary"`**, not `server_streaming`. The unary service stub dispatches it through the generic gRPC-web caller `_.kE`:

```
_.k.HV=function(a,b,c){return _.kE(this.ha,this.ka+"/$rpc/google.internal.communications.instantmessaging.v1.Messaging/PullMessages",a,b||{},l6a,c)};
_.k.sendMessage=function(a,b,c){return _.kE(this.ha,this.ka+".../SendMessage",...)};
_.k.cna=function(a,b,c){return _.kE(this.ha,this.ka+".../AckMessages",...)};
_.k.Tfa=function(a,b,c){return _.kE(this.ha,this.ka+".../PrewarmReceiver",...)};
_.k.Ypa=function(a,b,c){return _.kE(this.ha,this.ka+".../Echo",...)};
```

`ReceiveMessages` is **not** in this unary stub; it is wired through a distinct streaming handler (`m6a` + `T6a(...)`, `.../ReceiveMessages",a,b||{})`). So:

- **Modern receive path = PullMessages (unary) + PrewarmReceiver.** A hanging/long-poll unary call stays open for minutes in the network panel and returns one batch, then is re-issued — this fully explains the observed "long-lived, rarely re-issued" PullMessages **without** it being a gRPC server-stream. (Confidence HIGH that PullMessages is unary; the long-poll interpretation is MED — inferred, not proven from code.)
- **ReceiveMessages (server_streaming) is the legacy path** still present in the bundle (this is what mautrix/libgm uses).
- Both `StartFallbackToTachyonPullMessages` / `StopFallbackToTachyonPullMessages` Ditto counters exist, i.e. the client can *fall back* between the two receive transports.

## Message schemas (proto3-style; only client-touched fields)

Shared header types:
- `RequestHeader` = `_.fz`; `ResponseHeader` = `_.Fz`; streaming header = `vTa` (distinct type used only by `ReceiveMessages` response).

```proto
// _.fz  — HIGH for field 1; rest via _.Gz descriptor (see bottom, MED)
// snippet: _.fz=class extends _.n{...Uf(){return _.q(this,1)}}
message RequestHeader {
  string field1 = 1;   // _.q(this,1)  (some id/token string) — HIGH
  // other fields per _.Gz descriptor: field2 (double?), field100 (repeated msg) — MED, see below
}

// _.Fz  — no named getters anywhere: fully opaque
// snippet: _.Fz=class extends _.n{constructor(a){super(a)}}
message ResponseHeader { /* UNKNOWN fields */ }

// vTa — ReceiveMessages streaming response header; no getters
message StreamHeader { /* UNKNOWN */ }
```

### Messaging.SendMessage (HIGH for listed fields)

```proto
// _.QD=class extends _.n{...Wg(){return _.T(this,_.Ez,2)} setMessage(a){return _.U(this,2,a)}
//                          getHeader(){return _.T(this,_.fz,3)} ...hasHeader...rq(this,_.fz,3)}
message SendMessageRequest {          // _.QD
  OutgoingMessage message = 2;        // _.T(this,_.Ez,2)  — HIGH
  RequestHeader   header  = 3;        // _.T(this,_.fz,3)  — HIGH
}
// _.iD=class{...getHeader(){return _.T(this,_.Fz,1)}...}
message SendMessageResponse {         // _.iD
  ResponseHeader header = 1;          // HIGH
  // UNKNOWN: message-id / timestamp fields not read by client
}
// _.Ez=class{...Wg(){return _.un(this,12)} setMessage(a){return _.Uq(this,12,a)}}
message OutgoingMessage {             // _.Ez
  bytes payload = 12;                 // _.un(this,12) = ByteString — HIGH (encrypted envelope)
  // UNKNOWN: recipient/conversation/type fields (opaque or set via generic paths)
}
```

### Messaging.PullMessages (HIGH for header; body encrypted → UNKNOWN)

```proto
// tTa=class{...getHeader(){return _.T(this,_.fz,1)}...}
message PullMessagesRequest {         // tTa
  RequestHeader header = 1;           // HIGH
  // UNKNOWN: ack/cursor/receiver-config fields
}
// k6a=class{...getHeader(){return _.T(this,_.Fz,1)}...}   (deserialized per-batch: _.wd(k6a))
message PullMessagesResponse {        // k6a
  ResponseHeader header = 1;          // HIGH
  // UNKNOWN: repeated pulled-message / encrypted-blob field(s) — client never reads them by name
}
```

### Messaging.AckMessages (HIGH)

```proto
// iTa=class{...getHeader(){return _.T(this,_.fz,1)}...}
message AckMessagesRequest {          // iTa
  RequestHeader header = 1;           // HIGH
  // UNKNOWN: repeated message-id / ack-entry field(s)
}
// _.$3a=class{...getHeader(){return _.T(this,_.Fz,1)}...}
message AckMessagesResponse {         // _.$3a
  ResponseHeader header = 1;          // HIGH
}
```

### Messaging.ReceiveMessages (legacy streaming; HIGH for header)

```proto
// uTa=class{...getHeader(){return _.T(this,_.fz,1)}...}
message ReceiveMessagesRequest {      // uTa
  RequestHeader header = 1;           // HIGH
}
// _.Jz=class{...getHeader(){return _.T(this,vTa,1)}...}
message ReceiveMessagesResponse {     // _.Jz  (server_streaming)
  StreamHeader header = 1;            // type vTa — HIGH
  // UNKNOWN: message body field(s)
}
```

### Registration.SignInGaia (HIGH for listed)

```proto
// u7a=class{...getHeader(){return _.T(this,_.fz,1)}...}
message SignInGaiaRequest {           // u7a
  RequestHeader header = 1;           // HIGH
  // UNKNOWN: device/app/GAIA-token fields
}
// _.k4a=class{...getHeader(){return _.T(this,_.Fz,1)}... ZR(){return _.T(this,_.Hz,3)}}
message SignInGaiaResponse {          // _.k4a
  ResponseHeader header = 1;          // HIGH
  RegistrationData field3 = 3;        // _.T(this,_.Hz,3) — HIGH (type _.Hz, body UNKNOWN; likely auth/registration token)
}
message Hz { /* UNKNOWN — no getters */ }
```

### Registration.ListIdentities (HIGH for header; list opaque)

```proto
// _.k7a=class{...getHeader(){return _.T(this,_.fz,1)}...}
message ListIdentitiesRequest {       // _.k7a
  RequestHeader header = 1;           // HIGH
}
// _.l7a=class{...getHeader(){return _.T(this,_.Fz,1)}...}
message ListIdentitiesResponse {      // _.l7a
  ResponseHeader header = 1;          // HIGH
  // UNKNOWN: repeated Identity field — not read by named getter
}
// Identity type _.cz (used in SignInSecondary.getId): getType(){return _.tn(this,1)} getId(){return _.q(this,2)}
message Identity {                    // _.cz
  IdentityType type = 1;              // _.tn(this,1) enum — HIGH
  string       id   = 2;              // _.q(this,2) — HIGH
}
```

### MessagesMultiDevice.GetFiUserStanding (HIGH for header; body opaque)

```proto
// _.Jab=class{...getHeader(){return _.T(this,_.fz,1)}...}
message GetFiUserStandingRequest {    // _.Jab
  RequestHeader header = 1;           // HIGH
}
// _.dD=class{...getHeader(){return _.T(this,_.Fz,1)}...}
message GetFiUserStandingResponse {   // _.dD
  ResponseHeader header = 1;          // HIGH
  // UNKNOWN: standing/status enum field
}
```

## Header descriptor `_.Gz` (MED-LOW — array-format decode, unverified interpreter)

`_.Gz` is the reusable header field-map, embedded as the `header` submessage in typed descriptors, e.g. `_.td(_.c4a,[0,_.Gz,_.Er])`, `_.td(_.e4a,[0,_.Gz,_.Er,_.qr])`.

```
_.Gz=[0,1,_.eza,1,_.qr,98,[0,_.Jr,-1]];
```

Best-effort decode using the observed `[pivot, (skip,codec)...]` convention (pivot 0; a leading number adds to the running field number, a negative number = repeated):

```proto
message HeaderGz {                    // MED-LOW: format decode not verified against interpreter Wc()
  <eza> field1  = 1;    // codec _.eza
  <qr>  field2  = 2;    // codec _.qr (numeric/double family)
  repeated <Jr-submessage> field100 = 100;  // 2+98=100, negative => repeated
}
```

Codec→type calibration (from types where descriptor + getter both known): `_.Er` ↔ `_.un` = **bytes**; `_.qr` ↔ `_.Vo` = **double/number**; `_.Jr`/`_.eza` = string-family. These are consistent but not individually proven, hence MED.

## Confidence summary & gaps

- HIGH: full RPC catalog (names, services, unary-vs-streaming, req/resp type identifiers); every `header` field number+type; the specific extra fields listed above (SendMessage msg=2/hdr=3, OutgoingMessage payload=12, SignInGaia resp field3, Identity type=1/id=2).
- HIGH-derived: modern receive = **unary** PullMessages (+PrewarmReceiver); ReceiveMessages is the separate legacy server-stream; client can fall back between them.
- MED: PullMessages behaving as a hanging long-poll (explains long-open request); `_.Gz` header field decode.
- **Biggest gaps (UNKNOWN):** all encrypted payload bodies — PullMessages/ReceiveMessages message-batch fields, AckMessages ack-entry list, SendMessage recipient/conversation fields, ListIdentities identity list, GetFiUserStanding standing enum, SignInGaia device/token fields, and `ResponseHeader (_.Fz)` internals. The schema-less decoder means these never surface as named getters; recovering them needs the .proto or captured plaintext, not this bundle.

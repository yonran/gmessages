# libgm baseline — what already exists, and the PullMessages delta

Package: `/Users/yonran/repos/gmessages/pkg/libgm` (checkout `v0.2602.0-31-g82f0755`,
upstream mautrix/gmessages plus local `DontMarkActive` patches).

This catalogs the EXISTING implementation so the modern-API ("PullMessages") work
reuses it and adds only the delta. All line numbers are as of this checkout.

Ground truth captured this session (method NAMES only; encrypted payloads NOT
observable): the live messages.google.com/web client uses
`.../v1.Messaging/{SignInGaia?,GetFiUserStanding,ListIdentities,SendMessage,PullMessages,AckMessages}`
and receives via a long-lived **server-streaming PullMessages**. libgm today
receives via **ReceiveMessages**. Send + Ack endpoint NAMES are shared between old
and new. `GetFiUserStanding` / `ListIdentities` / `PullMessages` are absent from libgm.
`SignInGaia` exists in libgm but on the **Registration** service, not Messaging.

---

## (a) gmproto messages/enums already defined (receive / send / ack / auth / session)

### Transport envelope & routing — `gmproto/rpc.proto`
- `StartAckMessage` — rpc.proto:10 (startup skip `count`)
- `LongPollingPayload` — rpc.proto:14 (streaming frame: `data`=2 / `heartbeat`=3 / `ack`=4 / `startRead`=5)
- `IncomingRPCMessage` — rpc.proto:21 (`responseID`, `bugleRoute`, `messageType`, `messageData` bytes=12)
- `RPCMessageData` — rpc.proto:47 (`sessionID`, `action`, `unencryptedData`=5, `encryptedData`=8, `encryptedData2`=11)
- `OutgoingRPCMessage` — rpc.proto:59 (`.Auth`=60 with `tachyonAuthToken`; `.Data`=67; `.Data.Type`=74; `mobile`, `TTL`, `destRegistrationIDs`)
- `OutgoingRPCData` — rpc.proto:91 (`requestID`, `action`, `unencryptedProtoData`=3, `encryptedProtoData`=5, `sessionID`=6)
- `OutgoingRPCResponse` — rpc.proto:99 (SendMessage/Ack HTTP reply; `timestamp` only on SendMessage)
- enum `BugleRoute` — rpc.proto:110 (`DataEvent`=19, `PairEvent`=14, `GaiaEvent`=7)
- enum `ActionType` — rpc.proto:117 (`SEND_MESSAGE`=3, `GET_UPDATES`=16, `NOTIFY_DITTO_ACTIVITY`=22, `ACK_BROWSER_PRESENCE`=17, gaia-pairing 42–47, etc.)
- enum `MessageType` — rpc.proto:170 (`BUGLE_MESSAGE`=2, `BUGLE_ANNOTATION`=16, `GAIA_2`=20)

### Receive / ack request bodies — `gmproto/client.proto`
- `ReceiveMessagesRequest` — client.proto:19 (`auth`=`AuthMessage`; nested `UnknownEmptyObject2{UnknownEmptyObject1}`=field 4) — **this is the OLD receive body**
- `AckMessageRequest` — client.proto:34 (`authData`=`AuthMessage`, `emptyArr`, repeated `Message{requestID, device}`)
- `NotifyDittoActivityRequest/Response` — client.proto:12/17
- `MessageReadRequest` — client.proto:29

### Auth / session / registration — `gmproto/authentication.proto`
- `AuthMessage` — authentication.proto:186 (`requestID`, `network`, `tachyonAuthToken`=6, `configVersion`) — the shared auth blob on every request
- `ConfigVersion` — authentication.proto:40; `Device` — :34; `BrowserDetails` — :27
- `SignInGaiaRequest` — authentication.proto:48 (`AuthMessage`, `Inner{DeviceID, Data someData}`, `network`)
- `SignInGaiaResponse` — authentication.proto:66 (`DeviceData{deviceWrapper, unknownItems2/3}`, `tokenData`)
- `GaiaPairingRequestContainer` / `GaiaPairingResponseContainer` — :86 / :130
- enum `GaiaPairingErrorCode` — :95
- `RPCGaiaData` — :144 (gaia event payload); `RevokeGaiaPairingRequest` — :140
- `AuthenticationContainer` — :176; `RegisterRefreshRequest` — :202 (`messageAuth`, `currBrowserDevice`, `unixTimestamp`, `signature`, push regs); `RegisterRefreshResponse` — :228
- `TokenData` — :294 (tachyon token + TTL); `RegisterPhoneRelayResponse`/`RefreshPhoneRelayResponse` (QR-pair path) — :232/:245
- ukey2 handshake protos live in `ukey.proto`; update/event payloads in `events.proto` (`UpdateEvents`, `LongPollingPayload.data` decrypts into these).

### URL constants — `util/paths.go`
- Messaging service base :19–20 (`...v1.Messaging`), with `ReceiveMessagesURL`:21, `SendMessageURL`:22, `AckMessagesURL`:23 (+ `...Google` clients6 variants :24–26).
- Registration service base :28 with `SignInGaiaURL`:29, `RegisterRefreshURL`:30.
- Pairing service base :13 (QR relay pairing).

---

## (b) Current receive path (ReceiveMessages long-poll)

1. `Client.Connect()` (client.go:220) → `go c.doLongPoll(true,false,c.postConnect)` (client.go:238) and `startAckInterval()` (client.go:239).
2. `doLongPoll(loggedIn, background, onFirstConnect)` — longpoll.go:304:
   - starts the `dittoPinger.Loop()` keepalive (longpoll.go:319) when logged in;
   - each iteration `refreshAuthToken(nil)` (longpoll.go:330);
   - builds `gmproto.ReceiveMessagesRequest{Auth:{RequestID, TachyonAuthToken, Network, ConfigVersion}, Unknown:{Unknown:{}}}` (longpoll.go:355);
   - POSTs to `util.ReceiveMessagesURL` (or `...Google` when `HasCookies()`) via `makeProtobufHTTPRequestContext(..., ContentTypePBLite, longPoll=true)` (longpoll.go:366-370) using the 30-min-timeout `lphttp` client;
   - stores `resp.Body` as `c.longPollingConn`, fires `onFirstConnect`, then `readLongPoll` (longpoll.go:430).
3. `readLongPoll` (longpoll.go:439) — streaming parse of a chunked JSON array:
   - expects opening `[[`, splits comma-separated pblite blocks, `json.Valid` gate to accumulate partial chunks;
   - `pblite.Unmarshal` each block into `gmproto.LongPollingPayload` (longpoll.go:504);
   - dispatch (longpoll.go:510): `.data`→`c.HandleRPCMsg`; `.ack`→sets `c.skipCount` (startup dedup); `.startRead`/`.heartbeat`→trace; `]]`→stream end.
4. `HandleRPCMsg` (event_handler.go:177) → `decryptInternalMessage` (event_handler.go:49): routes by `BugleRoute`; for `DataEvent` unmarshals `RPCMessageData`, looks up `responseType[action]` (event_handler.go:29), decrypts `encryptedData` (or `encryptedData2`) via `AuthData.RequestCrypto.Decrypt`, unmarshals into the typed response.
   - **every** message → `queueMessageAck(msg.ResponseID)` (event_handler.go:185);
   - `receiveResponse` matches pending RPC waiters by `SessionID` (session_handler.go:108) — returns early if it was a reply;
   - otherwise `handleUpdatesEvent` (event_handler.go:218) fans out `GET_UPDATES` `UpdateEvents` into message/conversation/typing/settings/presence events.
5. Acks: `queueMessageAck` (session_handler.go:257) appends to `ackMap`; `startAckInterval` (session_handler.go:268) flushes every 5 s via `sendAckRequest` (session_handler.go:281) → builds `AckMessageRequest` (auth + `acks[]{requestID, device}`) → POST `util.AckMessagesURL`.
6. `GET_UPDATES` keepalive/recovery: `HandleNoRecentUpdates` (longpoll.go:266) and `SetActiveSession` (methods.go:144) send `ActionType_GET_UPDATES` with the session UUID; `shouldDoDataReceiveCheck` (longpoll.go:280) drives the ~2h55m re-check.
7. Background one-shot variant: `ConnectBackground()` (client.go:243) runs `doLongPoll(true,true,nil)` which auto-closes after ~10 s idle (`readLongPoll` closeIn timer, longpoll.go:465) then flushes acks once.

## (c) Auth / registration paths present

- **Google-account (gaia) pairing**: `DoGaiaPairing` (pair_google.go:308) → `StartGaiaPairing` (:329) → `signInGaiaGetToken` (:81) POSTs `SignInGaiaRequest` to `util.SignInGaiaURL` (Registration service), stores `TokenData` via `updateTachyonAuthToken`, sets `Mobile`/`Browser`, picks primary device, runs ukey2 handshake over `CREATE_GAIA_PAIRING_CLIENT_INIT/FINISHED` RPCs → `FinishGaiaPairing` (:424) derives `RequestCrypto.AESKey/HMACKey`. `signInGaiaInitial` (:73) is the unused first-call variant. `UnpairGaia` (:533).
- **QR relay pairing** (non-google): `pair.go` (RegisterPhoneRelay/RefreshPhoneRelay via Pairing service) — separate legacy path.
- **Token refresh**: `refreshAuthToken` (client.go:432) — signs `requestID:timestamp` with `RefreshKey` (ECDSA), POSTs `RegisterRefreshRequest` to `util.RegisterRefreshURL`, updates tachyon token; called at the top of every long-poll iteration and in `Connect`. Buffer `RefreshTachyonBuffer`=1h (client.go:104).
- **AuthData** (client.go:27): holds `RequestCrypto` (AES-CTR), `RefreshKey` (JWK/ECDSA), `Browser`/`Mobile` devices, `TachyonAuthToken`+expiry+TTL, `Cookies`, `SessionID`/`DestRegID`/`PairingID`. `IsGoogleAccount()`=`DestRegID != Nil`; `HasCookies()`, `AuthNetwork()` (client.go:82-102).
- **Crypto helpers** — `crypto/`: `aesctr.go` (`AESCTRHelper.Encrypt/Decrypt`, the request payload cipher), `aesgcm.go`, `ecdsa.go` (refresh-key signing), `generate.go`. HKDF/ukey2 derivation inline in pair_google.go (`doHKDF` :182, `byteHash` :478).

## (d) Session handler — how outgoing RPCs are built / encrypted / signed

`SessionHandler` (session_handler.go:18) holds response waiters, the ack map, and the `sessionID`.
- `sendMessage`/`sendMessageWithParams`/`sendAsyncMessage`/`sendMessageNoResponse` (session_handler.go:171/151/55/35) are the four entry points; `methods.go` wraps them per action.
- `buildMessage` (session_handler.go:189) is the single builder:
  1. new `requestID` (uuid) unless provided; default `MessageType_BUGLE_MESSAGE`;
  2. wraps in `OutgoingRPCMessage` with `Mobile`, `Auth{RequestID, TachyonAuthToken, ConfigVersion}`, `DestRegistrationIDs` (from `AuthData.DestRegID`), `TTL` (`TachyonTTL` unless `OmitTTL`/`CustomTTL`);
  3. marshals `params.Data`, then **encrypts** with `AuthData.RequestCrypto.Encrypt` unless `DontEncrypt` (session_handler.go:234-241);
  4. embeds `OutgoingRPCData{requestID, action, unencrypted/encryptedProtoData, sessionID}` as `Data.MessageData`.
- Transport: `makeProtobufHTTPRequest` (http.go:26) → pblite/protobuf marshal → `BuildRelayHeaders` + cookies + `SAPISIDHash` Authorization (http.go:63) → POST. Send/Ack use `SendMessageURL`/`AckMessagesURL` (or `...Google`).
- Response correlation: `waitResponse`/`receiveResponse` match on `SessionID`==requestID (session_handler.go:93/108); 5 s soft timeout pokes `pingShortCircuit` (session_handler.go:160).
- Note: request "signing" per-message = the tachyon token + AES-CTR encryption; the only ECDSA signature is in the token-refresh call (client.go:441), not per RPC.

---

## (e) DELTA LIST — add a PullMessages receive path, REUSE send/ack/auth/crypto

Everything in (c)/(d) is reused unchanged: `AuthData`, `RequestCrypto`, `refreshAuthToken`,
`buildMessage`/`sendMessage*`, `queueMessageAck`/`sendAckRequest`, `decryptInternalMessage`/
`HandleRPCMsg`/`handleUpdatesEvent`, `dittoPinger`, the `LongPollingPayload` framing, and the
`SignInGaia` machinery. The receive envelope (`IncomingRPCMessage`/`RPCMessageData`/`BugleRoute`/
`ActionType`) is very likely identical on the wire — treat as reusable until a capture disproves it.

Concretely, ADD:

1. **Endpoint constants** — `util/paths.go`: add
   `PullMessagesURL = messagingBaseURL + "/PullMessages"` and
   `PullMessagesURLGoogle = messagingBaseURLGoogle + "/PullMessages"`.
   (SendMessage/AckMessages constants already exist and are shared; :19–26.)

2. **Request proto** — `gmproto/client.proto`: add `PullMessagesRequest` alongside
   `ReceiveMessagesRequest` (client.proto:19). Field layout is UNKNOWN (payload was encrypted
   in the capture) — start by cloning `ReceiveMessagesRequest` (`auth AuthMessage` + the
   `UnknownEmptyObject` filler) and refine against a real capture. Regenerate `client.pb.go`.

3. **New long-poll function** — `longpoll.go`: add `doPullMessages(...)` (or branch inside
   `doLongPoll`) that is identical to the current loop EXCEPT it builds `PullMessagesRequest`
   and POSTs to `PullMessagesURL`/`...Google` (longpoll.go:355-370). Response streaming is
   assumed identical: keep `readLongPoll` (longpoll.go:439) and its `LongPollingPayload`
   dispatch verbatim. If the modern stream framing differs (not `[[ … ]]` pblite), fork
   `readLongPoll`; otherwise reuse.

4. **Selector to choose the path** — `client.go`: gate `Connect()` (client.go:238) between
   `doLongPoll` (ReceiveMessages) and the new PullMessages loop, e.g. a `Client.UseModernAPI`
   / `UsePullMessages` bool (mirror the existing `DontMarkActive` flag pattern, client.go:143).

5. **(likely-needed) new ActionType / responseType entries** — `gmproto/rpc.proto` enum
   `ActionType` (rpc.proto:117) + `responseType` map (event_handler.go:29): the modern client
   also calls `GetFiUserStanding` and `ListIdentities`. If those ride the same
   Messaging-RPC/`OutgoingRPCMessage` mechanism they need action-enum values + response protos;
   if they are plain top-level RPCs (like SignInGaia) they instead need their own request/response
   protos + URL constants (`GetFiUserStandingURL`, `ListIdentitiesURL`) and small `methods.go`
   wrappers. Which one is TBD without a decrypted capture.

6. **Registration/SignInGaia** — reuse as-is. `SignInGaiaURL` already targets the Registration
   service (paths.go:29); the ground-truth "Messaging/SignInGaia" name is the observed host path
   but the existing gaia sign-in + token-refresh flow is believed sufficient to obtain the
   tachyon token PullMessages needs. Revisit only if PullMessages rejects the token.

NOT required: no new crypto, no changes to `buildMessage`, ack flushing, ditto pinger, or the
event fan-out. The delta is a new request body + endpoint + loop entry, plus possibly two
auxiliary RPC wrappers — all layered on the existing send/ack/auth/crypto stack.

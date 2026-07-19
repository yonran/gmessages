# CAP-03 — Device-registration diff: modern web vs libgm

Goal: find the device-registration field that plausibly makes Google **keep
notifying the phone** for the modern web client but **suppress the phone** for
libgm. Compared captured registration RPCs (`SignInGaia-46/47`,
`ListIdentities-49`, `LookupRegistered-87`, `GetFiUserStanding-48`) against how
libgm builds its registration (`pair_google.go`, `pair.go`, `client.go`,
`util/config.go`, `gmproto/authentication.pb.go`).

REDACTION: all real tokens/ids/numbers below are typed placeholders. Only
structure (array index → proto field number → type → meaning) is documented.

---

## 0. Headline finding

The `SignInGaia` device descriptor is **byte-identical** between the modern web
client and libgm — so SignInGaia is *not* the difference. The registration
difference lives in the **`BrowserDetails` message** sent at pairing time
(`GaiaPairingRequestContainer.browserDetails` / QR `AuthenticationContainer.browserDetails`),
specifically **`BrowserDetails.deviceType`**: libgm registers **`TABLET` (2)**,
the modern web client registers as a web/PWA companion (`WEB` (1) / `PWA` (3)).
A standalone-looking `TABLET` is the kind of device Google lets *own* the
conversation and suppress the phone; an ephemeral web session does not.

---

## 1. SignInGaia request — captured vs libgm (IDENTICAL)

Captured `SignInGaia-46.network-request` (JSPB), mapped to `SignInGaiaRequest`:

```
[ <AuthMessage f1>, <Inner f2>, <UnknownInt3 f3>, <Network f4> ]
 idx0 = AuthMessage:  [ <string:requestId>, null, "GDitto"(f3 network),
                        null,null,null, <ConfigVersion f7>=[.., 2026,3,18-ish,.., V1,.., V2] ]
 idx1 = Inner:        [ [ 3, "messages-web-<hex32:sessionId>" ] ]   # Inner.deviceID = [unknownInt1=3, deviceID]
 idx2 = UnknownInt3:  1
 idx3 = Network:      "GDitto"
```

libgm `baseSignInGaiaPayload()` + `signInGaiaInitial()` produce the same:
- `AuthMessage.Network = util.GoogleNetwork = "GDitto"` ✓
- `Inner.DeviceID.UnknownInt1 = 3` ✓  (proto field 1)
- `Inner.DeviceID.DeviceID = fmt.Sprintf("messages-web-%x", SessionID[:])` → `messages-web-<32 hex>` ✓  (proto field 2)
- `UnknownInt3 = 1` ✓ ; `Network = "GDitto"` ✓

So the "TYPE 3, name messages-web-<hash>" descriptor (`Inner.DeviceID = [3, name]`)
is emitted **identically** by libgm. This `3` is `SignInGaiaRequest_Inner_DeviceID.unknownInt1`
(proto f1), **not** the `DeviceType` enum. Not the lever.

Note: the captured request has **no** `Inner.someData` (proto f36 = pubkey),
i.e. it is the `signInGaiaInitial` variant (UnknownInt3=1). libgm currently only
calls `signInGaiaGetToken` (which *adds* `someData` at f36); `signInGaiaInitial`
is `//lint:ignore U1000` dead code. This extra field does not change the device
descriptor and is not the routing lever.

### SignInGaia response device list (structure only)

`SignInGaia-46.network-response` idx2 = DeviceData:
```
idx2[0] UnknownItems1: [[16, "<string:email>", "GDitto"]]          # account identity, type 16
idx2[1] UnknownItems2: list of [ <string:regUUID>, null, null, <int:acctDeviceType f4>,
                                 <string:lang?>, null, <bigint:idlo f7> ]
        observed acctDeviceType values: 1 (has "en-US" → primary Android phone),
                                        6, 6, 6 (companion / web devices)
idx2[2] UnknownItems3: list of [ <string:regUUID>, null, <int f3>, <int f4>,
                                 null, null, <int64:tsMicros f7>, <bytes:blob f8> ]
        observed (f3,f4): (1,19) primary, (6,6), (6,6), (6,19)
```
libgm reads only: `UnknownItems2` where `UnknownInt4 == 1` → primary phone regUUID
(pair target). It ignores the acctDeviceType of companions. **Server assigns a
web companion `acctDeviceType = 6`.** That server-side `6` is almost certainly
derived from the `BrowserDetails.deviceType` we send at pairing — the field we
can move.

---

## 2. BrowserDetails — the actual registration descriptor (WHERE THEY DIFFER)

`BrowserDetails` (`gmproto/authentication.pb.go`, sent in the ukey2 pairing
container at `pair_google.go:499` and QR at `pair.go:91`):

| proto field | name          | libgm value (`util/config.go`)        | modern web (inferred) |
|-------------|---------------|----------------------------------------|-----------------------|
| f1 `userAgent`   | UserAgent   | `Mozilla/5.0 (Linux; Android 14) … Chrome/146 …` (util.UserAgent) | real desktop Chrome UA |
| f2 `browserType` | BrowserType | **`OTHER` (1)**                        | `CHROME` (2)          |
| f3 `OS`          | OS          | **`"libgm"`**                          | `"Linux"`/`"Windows"`… |
| f6 `deviceType`  | DeviceType  | **`TABLET` (2)**                       | **`WEB` (1)** (or `PWA` (3)) |

`DeviceType` enum: `WEB=1, TABLET=2, PWA=3`. `BrowserType` enum:
`OTHER=1, CHROME=2, …`.

`BrowserDetails` is **not** re-sent on `RegisterRefresh` (that carries only
`CurrBrowserDevice` = `Device{userID,sourceID,network}`, no type). So the
device type is fixed **at pair time** — changing it requires re-pairing.

---

## 3. Ranked candidates (what controls phone-notification suppression)

1. **`BrowserDetails.deviceType` `TABLET`(2) → `WEB`(1)** — *most likely*.
   A paired "tablet" reads to Google as a standalone messaging device that owns
   the thread → suppress the phone. "messages-web" is a browser companion →
   phone keeps notifying. Single-field, matches the "messages-web" naming.
2. **`BrowserDetails.OS` `"libgm"` → `"Linux"`/`"Chrome OS"`** — a non-standard
   OS string could bucket the device into a different (authoritative) class.
   Low cost, plausibly cosmetic. Change alongside #1.
3. **`BrowserDetails.browserType` `OTHER`(1) → `CHROME`(2)** — similar bucketing
   risk as #2; the real web client is Chrome. Cosmetic-leaning.
4. **`RegisterRefreshRequest` push registration** (`client.go:472`):
   `MoreParameters.PushReg{Type:"messages_web", Url, P256Dh, Auth}` only sent
   when `c.PushKeys != nil`. Presence/absence of a live web-push subscription is
   a routing signal, but it drives *browser* push delivery, not phone
   suppression, and the framing (web keeps phone, libgm suppresses) points at
   device *class*, not push keys. Lower rank.
5. **`SignInGaia` descriptor** — ruled out (identical, §1).

---

## 4. ListIdentities / LookupRegistered / GetFiUserStanding — NOT prerequisites

These run on **different networks** and belong to the **RCS / carrier** path,
not the Google-account (`GDitto`) message stream:

- `ListIdentities-49`: header network `"RCS"`; body `[16,"<email>","RCS"]`.
  Response → `[[1,"<phoneNumber>","RCS"]]` (self RCS identity lookup).
- `LookupRegistered-87`: network `"RCS"`; looks up whether a **peer** phone
  number is RCS-registered, returns RCS feature tags (`+g.3gpp.iari-ref`,
  `rcse.im`, `msgfallback`, …). Per-recipient capability probe.
- `GetFiUserStanding-48`: network `"CMS"`; Google Fi account standing. Empty
  body `[[ <hdr> ]]`, response `[[null,"<int64:id>"]]`.

libgm is a `GDitto` Google-account bridge and legitimately **skips all three**.
None of them registers the companion device or influences whether the phone is
notified for account messages. They are not the lever and should not be
implemented for this purpose.

---

## 5. Minimal patch plan (try #1 first)

`pkg/libgm/util/config.go`, `BrowserDetailsMessage`:

```go
var BrowserDetailsMessage = &gmproto.BrowserDetails{
    UserAgent:   UserAgent,
    BrowserType: gmproto.BrowserType_OTHER,   // step 2: → BrowserType_CHROME
    OS:          "libgm",                     // step 2: → "Linux"
    DeviceType:  gmproto.DeviceType_TABLET,   // STEP 1: → gmproto.DeviceType_WEB
}
```

- **First change (single field):** `DeviceType: gmproto.DeviceType_TABLET` →
  `gmproto.DeviceType_WEB`.
- This value is consumed at pairing via `util.BrowserDetailsMessage` in
  `pair_google.go:499` (Gaia) and `pair.go:91` (QR) — no other edits needed.
- **Must re-pair** after the change (BrowserDetails is registered once at pair
  time; `RegisterRefresh` won't update it).
- Verify by re-reading `SignInGaia` response `UnknownItems2` acctDeviceType for
  the new regUUID: if it changes away from `6`, and the phone resumes notifying,
  #1 is confirmed. If unchanged, add step-2 (`OS`,`browserType`) and re-test.

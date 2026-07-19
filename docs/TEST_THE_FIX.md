# How to test the WEB-device notification fix

The candidate fix (register `BrowserDetails.deviceType = WEB` instead of `TABLET`)
takes effect **only after a fresh re-pair**, because `BrowserDetails` is sent to
Google once at pair time and never re-sent on reconnect. So the running,
already-paired session will keep behaving as `TABLET` until you re-pair with a
binary built from this branch.

## Steps (requires your phone for the re-pair)

1. Point `home.nix`'s openmessage derivation at the WEB build
   (yonran/openmessage @ a5abbcb, which pins gmessages @ 5b17248):

   ```nix
   rev        = "a5abbcb37784bd9b7533d3117316182635b97848";
   hash       = "sha256-fw0YP5VEziscqZeaNc3ISPTSWiYE9Qud88Thw8JuJCw=";
   vendorHash = "sha256-fCgApo6tVM57MoIc55YwohvRpLXVjm7tJxrcrbjcPWc=";
   ```

2. `home-manager switch --flake ~/repos/yondesktop/home-manager-config#aarch64`
   (Deploying alone changes nothing yet — the existing session is still a TABLET
   registration.)

3. **Re-pair** so the new WEB descriptor registers with Google. Stop the agent,
   run the pair flow, restart:
   ```
   launchctl bootout gui/$(id -u)/org.nix-community.home.openmessage
   openmessage pair            # or `openmessage pair --google` for the GAIA/emoji flow
   launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/org.nix-community.home.openmessage.plist
   ```

4. Send yourself a text and watch your phone.
   - **Phone notifies + message still appears in openmessage** → the WEB device
     type is the fix. Keep it.
   - **Phone still silent** → deviceType alone isn't enough; next levers (same
     file, `pkg/libgm/util/config.go`): `BrowserType_OTHER` → a real browser
     (e.g. CHROME), and `OS = "libgm"` → a realistic OS string. Change one at a
     time and re-pair to keep the result attributable.
   - **Messages stop syncing** → revert deviceType to TABLET and re-pair.

## Not the fix (deprioritized)

`UseModernReceive` / the `PullMessages` scaffold — live capture proved the modern
web client receives over `ReceiveMessages` (the endpoint libgm already uses);
`PullMessages` is only a `[null,null,1]` heartbeat. Leave `UseModernReceive` off.

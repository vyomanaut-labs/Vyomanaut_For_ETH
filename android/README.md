# Vyomanaut Provider (Android)

Turns an Android phone into a storage provider for the Vyomanaut network. The app does not reimplement the
provider: it bundles the Go provider from this repository (`cmd/provider`, built for linux/arm64 with the
pure-Go Badger engine) as `libprovider.so` and runs it as a child process with the same flags as the
Windows installer.

## Build

From this `android` folder, on Windows with Go and Android Studio installed:

```powershell
.\scripts\build_android_provider.ps1     # builds cmd/provider into app/src/main/jniLibs (not committed)
.\gradlew.bat assembleDebug testDebugUnitTest
```

Or open this `android` folder in Android Studio and run the `app` configuration. Without the first step the
app still builds, and the Home tab says the provider program is not bundled.

Only `arm64-v8a` phones are supported (no 32-bit phones, no x86 emulators).

## Reaching a phone: the relay

Android never delivers new incoming connections over a VPN such as ZeroTier to an app. It was tested on a POCO M6
Plus (Android 14) and a Galaxy M34 (Android 16): a listener running as the app timed out, while the same listener
running as the shell user, or reached over USB tethering, accepted the connection. Heartbeats still work, because
the phone opens them itself. Audits, vetting chunks and uploads do not, because they must connect *into* the phone.

So phones stay reachable through a relay node, which is ARCH §13 Tier 3 (`internal/p2p/relay.go`):

1. On the coordinator machine, run the relay next to the coordinator:

   ```bash
   go run ./cmd/relay --listen :30300
   ```

   On macOS, allow incoming connections for it if the firewall asks.
2. The app starts the provider with `--relay-addrs=<coordinator host>:30300`. Change it on the Network tab, or set it
   to `off`. The provider keeps an outgoing reservation on the relay. Its heartbeats advertise
   `/ip4/<relay>/tcp/30300/p2p-circuit/p2p/<peer id>` first.
3. The coordinator and clients dial that address with the normal p2p calls. The relay asks the phone to open a second
   outgoing connection and splices the two. TLS runs end to end, so the relay only sees ciphertext.

Checked end to end on the POCO: the real provider, running as the app, accepted an audit-challenge stream and a
chunk-upload stream through the relay in about 30 ms. The Windows providers need nothing: without
`--relay-addrs` the provider behaves as before.

## Tabs

| Tab | What it does |
|---|---|
| Home | Status (starting / online / reconnecting / offline), Start and Stop, registration with the 6-digit code, the "Before you start" battery checklist, and the provider output (kept on disk across restarts). |
| Payments | Unpaid ledger balance, audit streak and recent audits from the coordinator; how payouts work; the coordinator wallet and escrow, read directly from Whitechain. |
| Storage | How much space the provider offers (1 GiB up to 10% of free space). |
| Network | Coordinator address and phone number, connection test, ZeroTier check. |

## What changed from the first app (review of 6 Nov 2026)

| Review item | Fix |
|---|---|
| Only works while charging | `service/PowerLocks.kt` holds a partial wake lock and Wi-Fi locks while the service runs. Battery exemption is required before Start, plus a ZeroTier exemption step and phone-maker steps (`power/`). |
| 3b Restart race | `ProviderProcess.stopAndWait()` waits up to 5 s for the Go provider's clean SIGTERM shutdown, then kills it. Start and stop run on one background thread, so a new provider never starts before the old one has exited. |
| 3a Dead provider shown as online | `ProviderEngine` restarts the provider with backoff (5, 15, 30, 60, then 120 s; gives up after 10 failures in a row). The status and the notification follow the engine's real state. |
| 3c Departure never handled | `ProviderLogRules` spots `PROVIDER_DEPARTED` and the 401 "registration does not match" heartbeat lines. The app stops the provider and offers Register again; there is also Reset registration. |
| No network callback | `service/NetworkWatcher.kt` restarts the provider when ZeroTier comes back or its address changes, and skips any pending restart wait. |
| Wrong VPN address | `network/AddressPicker.kt` advertises the VPN address on the coordinator's subnet. |
| Logs lost on restart | `engine/LogFile.kt` keeps the last 200 lines in `files/provider-log.txt`. |
| `com.example` package | Now `org.vyomanaut.provider`. |
| Provider never started after registering (found in testing) | The provider starts as soon as registration succeeds. Register is disabled until the checklist is done. |
| Audits and uploads cannot reach the phone over ZeroTier (found in testing) | Relay node (see above); the phone advertises its relay address. |

Still open (outside the app):
- The coordinator's 10 GB minimum (`internal/api/provider.go`, migration 001) still inflates small phones.
- Cleartext HTTP is allowed for the ZeroTier demo.
- Registration still uses the operator-read code.
- The provider's 7-day login token is not renewed.
- No coordinator API returns per-provider on-chain payouts or the coordinator wallet; the app reads the wallet from the chain instead.
- Relay reservations are not signed yet. A peer could reserve another phone's Peer ID, but the TLS check means it only blocks that phone; it cannot read or forge traffic.

## Test on a phone

On Xiaomi/MIUI first enable Developer options > "Install via USB" and accept the prompt on the phone.

1. Fresh start: Network tab (test the coordinator), Storage tab (save a limit), Home > Register, enter the code.
   Start stays disabled until the checklist is done.
2. Unplugged, screen off, 30+ minutes: the coordinator should log no missed audits. To simulate Doze
   quickly, run `adb shell dumpsys deviceidle force-idle`, then `adb shell dumpsys deviceidle unforce` afterwards.
   Check the locks are held: `adb shell dumpsys power | grep Vyomanaut`.
3. Quick Stop then Start several times: no "address already in use" or "cannot acquire directory lock" in the output.
4. Kill the provider (`adb shell pkill -f libprovider.so`): Home shows "reconnecting" and it comes back within 5 s.
5. Turn ZeroTier off for a minute, then on: the provider restarts by itself.
6. Restart the coordinator (fresh database): within about a minute Home shows "register again".
7. If it dies on Android 12+, check `adb logcat | grep -i phantom` (the phantom process killer).
8. Payments tab: earnings load. With on-chain settlement on (`VYOMANAUT_CHAIN_ENABLED=1`, chain API on :8090),
   the coordinator wallet and escrow appear with explorer links.

# VHS tape — mdm-client: send DeviceInformation and show the typed response.
#
# Record:  vhs docs/demos/deviceinfo.tape   →   docs/demos/deviceinfo.gif
# Run from the repository root so the relative Output path resolves.
#
# Prereqs:
#   - `mdm-client` on PATH (`go install ./cmd/mdm-client`), or change the launch
#     line to `go run ./cmd/mdm-client`.
#   - A real enrolled device that is awake / reachable.
#   - Paste a real admin JWT where marked below, record, then REVERT this file —
#     do NOT commit the token. It is masked on screen as ***, but it sits in the
#     tape source until you revert.

Output docs/demos/deviceinfo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 760
Set Padding 20
Set Theme "Catppuccin Mocha"
Set FontFamily "JetBrains Mono, Noto Sans Mono CJK TC"

# Connection settings off-camera (keeps the frame clean; not sensitive).
Hide
Type "export MDM_URL=https://mdm.flarex.io NATS_URL=wss://nats.flarex.io NATS_CREDS=$HOME/.flarex/mdm/user.creds" Enter
Show

Type "mdm-client" Enter
Sleep 2s

# 1. Admin token — paste a real JWT here (shown as ***). Revert after recording.
Type "PASTE_ADMIN_JWT_HERE"
Sleep 1s
Enter
Sleep 2s

# 2. Device list — select the enrolled device (id + status only).
Sleep 1s
Enter
Sleep 1500ms

# 3. Command — DeviceInformation is the first entry.
Enter
Sleep 1s

# 4. Pick non-identifying queries: OSVersion, ProductName, ModelName, BatteryLevel.
Down
Space
Down 2
Space
Down
Space
Down 5
Space
Sleep 1500ms
Enter

# 5. Wait for the device to respond, then hold on the result.
#    Tune this to your device's round-trip; too short cuts off before the reply.
Sleep 12s

Type "q"
Sleep 1s

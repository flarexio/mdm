# Demo recordings

The GIF embedded in the top-level [README](../../README.md) is generated from the
[VHS](https://github.com/charmbracelet/vhs) `.tape` script in this folder, so it
is reproducible — edit the tape, re-record, commit the GIF.

## Prerequisites

- `vhs` (`go install github.com/charmbracelet/vhs@latest`, plus its `ttyd` and
  `ffmpeg` runtime deps — see the VHS README).
- The `mdm-client` binary on `PATH` (`go install ./cmd/mdm-client`).
- A real enrolled device that is awake/reachable, and NATS access (`NATS_CREDS`).

## Record

Run from the repository root so the relative `Output` path resolves:

```bash
vhs docs/demos/deviceinfo.tape   # docs/demos/deviceinfo.gif
```

## The admin token

The tape pastes the token from the system clipboard with `Ctrl+V`, so the JWT
never appears in the tape file (and there is nothing to revert). The TUI's masked
input reads the clipboard, so the token shows on screen only as `***`. Copy your
token before recording:

```bash
printf %s "$YOUR_ADMIN_JWT" | xclip -selection clipboard   # macOS: pbcopy
vhs docs/demos/deviceinfo.tape
```

`Ctrl+V` works because the recording machine has a clipboard. On a headless host
without one, fall back to typing the token literally (`Type "<jwt>"`) and revert
the tape afterwards.

## Privacy

The tape selects only non-identifying queries (OSVersion, ProductName, ModelName,
BatteryLevel) — not SerialNumber, UDID or the MAC addresses. The device picker
shows the enrollment id and status only (the UDID is intentionally not rendered).
If your enrollment id is itself sensitive, record against a test device with a
neutral id.

## Tuning

The `Sleep` after the query selection is sized for the device's round-trip
(push → poll → report). Too short and the GIF cuts off before the response
arrives; too long and it idles. The client's `--timeout` (default 30s) bounds the
wait.

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

`deviceinfo.tape` types a placeholder where the admin JWT goes. Before recording,
replace `PASTE_ADMIN_JWT_HERE` with a real token; the TUI masks it on screen
(`***`), but it sits in the tape **source** until you revert it. Record, then
**revert the file** — do not commit the token.

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

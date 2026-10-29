# ccu-addon-longpressdim

*[Deutsch](README.md)*

Dim lights and drive blinds by holding a key - **across interfaces**. An addon for
[openccu-lite](https://github.com/hobbyquaker/openccu-lite), OpenCCU and the CCU3.

## The problem

A direct link between a key and a dimmer exists only inside one interface: BidCos-RF with BidCos-RF, HmIP with HmIP,
BidCos-Wired with BidCos-Wired. An HmIP wall switch cannot dim a BidCos-Wired dimmer up and down, a BidCos-RF dimmer cannot be
driven from an HmIP key. Whoever wants it anyway takes the detour over the **virtual keys** (every interface has 50 of
them: `BidCoS-RF:1…50`, `BidCoS-Wir:1…50`, `HmIP-RCV-1:1…50`): the virtual key is linked to the dimmer directly, and
some logic relays the real key's presses to the virtual one.

This addon makes exactly that comfortable.

## What it does

- **Finds free virtual keys** of the actuator's interface (no link, no name, no other marker; from key 50 downwards)
  and **creates the direct links**: two keys per actuator, one for *on / brighter*, one for *off / darker* (*up* and
  *down* for blinds). The link parameters follow the WebUI's profiles; only what differs from what the actuator has is
  written.
- **Relays the presses:** a short press switches, a long press dims step by step while the key is held. Every relayed
  press is one step of the actuator; the addon mirrors the sender's repeats, at most one press per 300 ms, and drops
  what comes faster (faster presses queue in the interface process and the actuator overshoots).
- **One-key or two-key mode:** two keys (one on/brighter, one off/darker) or one key (short toggles, long dims up and
  down in turns; at the ends the last reported level decides).
- **Without a sender:** a binding can be created with no key at all - then an external logic (Node-RED, she, Home
  Assistant, a script) presses the virtual keys over RPC, see below.
- **Existing links** from virtual keys to dimmers and blinds that the addon did not create are shown and can be
  adopted (marked only, nothing is rewritten). The senders can be read from the `Mapping` function of a Node-RED flow.
- **HmIP keys report their presses only once a usage is registered** (`reportValueUsage`); on a CCU the logic layer
  does that for the programs, on openccu-lite this addon does it for its senders - registered when a binding gets the
  key, released when it loses it (switchable off in case another program needs the same key).
- **Step size per binding** (`LONG_DIM_STEP`, 5 % by default; the travel time per press for blinds), *on* to 100 % or to
  the last level, the dimming range.
- **Safe writes:** every change is shown as a plan first (which links are created, which parameters change, which
  keys are named) and then executed; the result is read back. An HmIP device that is out of reach takes the link later
  (`CONFIG_PENDING`); the addon shows *waiting for device* and keeps checking. It touches only keys it marked itself,
  and only links it created or adopted.

Supported are the dimmers and blind actuators of the three families: BidCos-RF (`DIMMER`, `BLIND`), BidCos-Wired
(`HMW_DIMMER`, `HMW_BLIND`) and HmIP/HmIPW (`DIMMER_VIRTUAL_RECEIVER`, `BLIND_VIRTUAL_RECEIVER`,
`SHUTTER_VIRTUAL_RECEIVER`). Sender and actuator must be on **different** interfaces - on the same one, the direct link
is the better choice.

## Installation

Install the package `longpressdim-<arch>-<version>.tar.gz` from the releases page through the system's Addons page.
The addon needs the scopes `rpc:read`, `rpc:operate`, `rpc:configure`, `meta:read` and `meta:write`: it reads devices,
links and the event stream, creates links, presses virtual keys and names them. It contacts nothing outside the
system.

The app is at `/addons/longpressdim/`, the settings page (the relay, *remove all bindings*) behind the addon's
settings button.

## On a CCU3 or OpenCCU

The same package installs as a classic addon (*Settings → Control panel → Additional software*, or
`/bin/install_addon` on OpenCCU). There the daemon talks to the interface processes directly - XML-RPC on the
loopback, with a callback on port 8294 for the key presses - and takes names and rooms from ReGaHSS; the marker of
the virtual keys it uses lives in the addon's own file (`var/markers.json`), their name is set in ReGaHSS. The addon's
*Settings* button opens the settings page, which opens the app. On a CCU3 with eQ-3's firmware the daemon starts
with the reboot after the install.

## Uninstalling

An uninstall leaves the direct links on the actuators and the names of the virtual keys in place - the dimmers keep
working with any other relay, and a silent removal of many links at uninstall time would be the riskier path. To be rid
of everything, use *Remove all bindings* on the settings page first. The settings (`etc/`) and the list of bindings
(`var/`) stay under `/usr/local/addons/longpressdim/` for a reinstall.

## Pressing the virtual keys from outside

For a binding without a sender (or beside its senders) an external logic presses the binding's two virtual keys - the
app shows them per binding. The rule: **short** = one `PRESS_SHORT`; **hold** = `PRESS_LONG` every ~300 ms on the
direction's key while held, nothing at release. Every press is one step; faster than every 300 ms queues in the
interface process.

Over lite-rpc (an API token with `rpc:operate`):

```sh
curl -u :<token> -X POST https://<system>/api/rpc/v1/json/BidCos-Wired \
  -d '{"jsonrpc":"2.0","method":"setValue","params":["BidCoS-Wir:48","PRESS_LONG",true],"id":1}'
```

Over XML-RPC (when the classic RPC access is on): `setValue("BidCoS-Wir:48", "PRESS_LONG", true)` on port 2000 (wired),
2001 (BidCos-RF) or 2010 (HmIP). Over hm2mqtt.js: `<prefix>/set/<key name>/PRESS_LONG` with `true`. In Node-RED: a
`ccu-value` node with the datapoint `PRESS_LONG`.

## Development

A Go backend on [ccu-addon-kit](https://github.com/hobbyquaker/ccu-addon-kit) (`go test -race ./...`), the UI in
Svelte 5 on [occulite-ui](https://github.com/hobbyquaker/occulite-ui) (`cd ui && npm ci && npm run check && npm test &&
npm run build`), the packages with `VERSION=x.y.z scripts/build.sh`. The API is described in [docs/api.md](docs/api.md)
and specified in `docs/openapi.json`.

The profile table (`internal/profiles`) is the addon's own: per family the link parameters derived from the WebUI's
profiles (*on/brighter*, *off/darker*, *toggle*).

## Licence

MIT

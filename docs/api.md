# The addon's API

Served at `/addons/longpressdim/api/v1/` behind the system's session (openccu-lite's gate confirms it; on a classic
CCU the kit's session check). Writes carry the kit's CSRF header `X-CCU-Addon-Request: 1`; the routes marked *admin*
need an administrator session. `openapi.json` (served at `api/v1/openapi.json`) is the specification; a test holds
the server's routes to it.

**Every write to the system is planned first.** `POST bindings/plan` answers the dry run - the links to create, the
link parameters that differ from what the actuator has, the keys to name and mark - and `POST bindings` executes the
same plan and answers the outcome per step (`done`, `pending`, `skipped`, `failed`). An HmIP process that answers
`TIMEOUT` for an `addLink` or `putParamset` has taken the write for a device it cannot reach now: the step is
`pending`, the binding's status *pending* until the device has it (`CONFIG_PENDING`), and the addon checks again
every minute.

| Route | What |
| --- | --- |
| `GET health` | `{ok, version, platform, uptime_s, service: {stream, stream_detail, bindings, presses, pending, changed}}` |
| `GET interfaces` | the interfaces with a virtual remote (`BidCos-RF`, `BidCos-Wired`, `HmIP-RF`) |
| `GET targets` | the dimmer and blind channels of every interface: `{interface, address, type, device, family, kind, name, rooms}` |
| `GET senders` | the key channels of every interface (`PRESS_SHORT`/`PRESS_LONG` in their VALUES), the virtual remotes left out |
| `GET keys/{interface}` | the 50 virtual keys: `{n, address, name, links, marker, foreign, free}` - *free* is no link, no name, no marker, no other consumer's metadata |
| `GET bindings`, `GET bindings/{id}` | the bindings as views: the binding, the names of target, keys and senders, the status of the last check (`active`, `pending`, `changed`, `missing-link`, `error`, `unknown`), the relay's activity (last hold, steps, dropped presses, level) |
| `POST bindings/plan` | the dry run of a draft: `{target: {interface, address}, mode?, up?, down?, senders?, options?, step_interval_ms?}` - the keys are picked from the free ones (50 downwards) when absent, the id from the target address |
| `POST bindings` *(admin)* | plan, write, store; `201` with the result, `422` when the plan fails (a key in use, no free keys, a sender on the target's interface), `502` with `{result}` when a write failed - the binding is stored when a link was created, so it can be reapplied or removed |
| `PATCH bindings/{id}` *(admin)* | `{senders, step_interval_ms}` - no link write; an HmIP sender's value usage is registered, a removed sender's released |
| `DELETE bindings/{id}[?forget=1]` *(admin)* | removes the links, the markers and the addon's names; `forget` leaves the links |
| `GET bindings/{id}/remove-plan[?forget=1]` | the dry run of the removal |
| `POST bindings/{id}/reapply` *(admin)* | writes the profile again (after a change elsewhere); an adopted binding becomes the addon's own |
| `POST bindings/{id}/press` | `{direction: up\|down, datapoint: PRESS_SHORT\|PRESS_LONG}` presses one of the binding's keys |
| `POST bindings/remove-all[?forget=1]` *(admin)* | every binding (the settings page) |
| `GET foreign` | links from virtual keys to dimmers and blinds that no binding owns, each with its paramset's classification (`long`: up, down, toggle, none; `short`: on, off, toggle, none; `role`, `step`, `on_level`) |
| `POST foreign/adopt` *(admin)* | `{interface, target, up, down, senders?}` adopts two such links: the keys are marked, nothing is written to the links; the mode and step come from the paramsets |
| `POST import/parse` | `{text}` - the `'<sender name>': '<key name>'` pairs of a pasted Node-RED mapping, resolved through the system's names; `{entries: [{sender_name, key_name, sender, key_interface, key, binding, role, error}]}` |
| `POST import/apply` *(admin)* | `{entries}` adds the resolved senders to their bindings (an entry with an error is skipped) |
| `POST check` | compares every binding with its profile now and answers the views |
| `GET settings`, `PUT settings` *(admin)* | `{step_interval_ms, hold_timeout_ms, log_presses, keep_usages}` |
| `GET stream` | server-sent events: `press` (any key channel's `PRESS_*`, for the editor's *press the button now*), `level`, `binding`, `check` |

## Value usages

An HmIP key channel reports its presses to the system only once a usage is registered for it: on a CCU ReGa calls
`reportValueUsage(<channel>, <datapoint>, <count>)` for every program that uses the key. openccu-lite has no ReGa, so
the addon reports `PRESS_SHORT` and `PRESS_LONG` with the count 1 for every HmIP sender of a binding, and `0` when the
sender leaves (unless another binding uses it, or `keep_usages` is set). The outcome is on the sender (`usage:
{state: reported|pending|failed|released, detail, at}`); `pending` means the process keeps the usage until the device
can be reached, and the addon reports again at its next check. The count the process keeps is one number per
datapoint, not per client: a key that another program (Node-RED, HomeKit) relies on as well should keep its usage
(`keep_usages`).

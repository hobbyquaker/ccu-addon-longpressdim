# Changelog

## [0.1.6] - 2026-11-23

- **OpenCCU:** a first installation through the WebUI no longer breaks off. The install script restarted the web
  server to read the addon's proxy rule while the WebUI's install request was still running on it, so the request was
  cut off: the WebUI showed "Error while loading the following file: /config/cp_software.cgi" (OpenCCU 3.89.14) or
  hung, no reboot followed and the addon did not start. It now reloads the web server gracefully. An update was not
  affected.

## [0.1.5] - 2026-11-22

- **Every system:** the addon takes events only from the interfaces where buttons and dimmers or blinds can be
  bound (BidCos-RF, BidCos-Wired, HmIP-RF). Before, it was registered for the events of every interface, among them
  VirtualDevices (the HmIP groups), whose events it never uses: that interface had to deliver each of them to the
  addon for nothing, and the addon appeared among its subscribers. **OpenCCU and CCU3:** a registration an earlier
  version left behind on the other interfaces is removed when the addon starts.

## [0.1.4] - 2026-11-17

- **openccu-lite:** a program can use the addon's API with an API token of the scope `addon:longpressdim` (made under
  *System → Remote access → API tokens*), without a login; it counts as a user's session, and a Full-access token as
  an administrator's. Before, the addon refused every token although the system let it through.

## [0.1.3] - 2026-10-28

- **Every system:** after an update the browser shows the new version of the app at the next normal reload. The
  packages are built reproducibly, so every file has the same date in every version; the app's `index.html` was
  answered by that date, the browser's check said "unchanged", and it kept the old page until its cache was cleared.
  The page is now checked by its content.
- **openccu-lite:** a binding's metadata step no longer fails when the key's name and marker are already what it
  writes (the system answers such a write with "not modified", which the addon took for an error).
- The interface processes on a classic CCU and the metadata on openccu-lite are now reached through ccu-addon-kit, the
  addon's own clients for them are gone.

## [0.1.2] - 2026-10-28

- **openccu-lite:** the addon comes up by itself after a reboot. The system's web server answers every API request
  with 503 "starting" for a few seconds after boot, until the system service is up; the daemon took that answer for
  "not openccu-lite", exited and stayed failed until started by hand from the Addons page. It now waits for the system
  (a log line when the wait begins, then one a minute) and keeps waiting on openccu-lite whatever the web server
  answers; only on a system that is not openccu-lite is a missing API still the end.

## [0.1.1] - 2026-10-27

- **openccu-lite:** the Addons page shows the addon's icon before the install. The icons are now in the repository at
  the path the manifest names, where the system's catalogue check reads them; before, it found none, showed the first
  letter instead and logged a warning per icon at every check.
- **openccu-lite:** the uninstall no longer tries to remove the system's files (the links, the proxy rule) and prints no
  "Permission denied" lines; the system removes them itself.
- **CCU3 and OpenCCU:** the uninstall restarts the web server when it removed the addon's proxy rule, so
  `/addons/longpressdim/` answers 404 right away instead of 503 until the next restart, and removes the rule's
  directory when it was the last file in it.
- The page shown while the service is not running names the addon *Long press dim*, as everywhere else.

## [0.1.0] - 2026-10-27

The first release.

- Bindings: two free virtual keys of the actuator's interface linked to a dimmer or blind actuator - one for
  *on / brighter* (*up*), one for *off / darker* (*down*) - with the link parameters of the WebUI's profiles, only
  the differences written; two-key and one-key mode (short toggles, long dims up and down in turns).
- The relay: the senders' presses on other interfaces (BidCos-RF, BidCos-Wired, HmIP) forwarded to the keys - one
  step per press, at most one per 300 ms, the surplus dropped, nothing at release.
- HmIP senders get their value usage registered (`reportValueUsage`), so they report their presses on openccu-lite;
  released when the binding loses them (`keep_usages` keeps them).
- Bindings without a sender for external logic, with the keys and copyable lite-rpc / XML-RPC / hm2mqtt.js /
  Node-RED examples.
- Found links: links from virtual keys the addon did not create are shown, classified and adoptable; the senders of
  a Node-RED mapping can be imported.
- Every write planned and shown first; `CONFIG_PENDING` and HmIP's `TIMEOUT` handled as pending; drift (changed
  elsewhere, link missing) shown with *write again*.
- The settings page: step interval, hold timeout, press log, *remove all bindings*.
- openccu-lite (the addon's token and session), OpenCCU and the CCU3 (XML-RPC, ReGaHSS). Dimmers and blinds of
  BidCos-RF, BidCos-Wired and HmIP/HmIPW; tested on an HmIP dimmer - the BidCos-RF and wired dimmers and the blinds
  follow the WebUI's profile tables.

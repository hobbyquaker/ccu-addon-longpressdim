# Changelog

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

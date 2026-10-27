# ccu-addon-longpressdim

*[English](README.en.md)*

Dimmen und Rollläden fahren per langem Tastendruck - **über Schnittstellen hinweg**. Ein Addon für
[openccu-lite](https://github.com/hobbyquaker/openccu-lite), OpenCCU und die CCU3.

## Das Problem

Eine Direktverknüpfung zwischen einem Taster und einem Dimmer gibt es nur innerhalb einer Schnittstelle: BidCos-RF
mit BidCos-RF, HmIP mit HmIP, BidCos-Wired mit BidCos-Wired. Ein HmIP-Wandtaster kann einen Wired-Dimmer also
nicht direkt hoch- und runterdimmen, und ein BidCos-RF-Dimmer nicht von einem HmIP-Taster bedient werden. Wer es
trotzdem will, baut sich den Umweg über die **virtuellen Tasten** (jede Schnittstelle hat 50 davon: `BidCoS-RF:1…50`,
`BidCoS-Wir:1…50`, `HmIP-RCV-1:1…50`): die virtuelle Taste wird mit dem Dimmer direkt verknüpft, und eine Logik
leitet die Tastendrücke des echten Tasters an die virtuelle weiter.

Dieses Addon macht genau das bequem.

## Was es tut

- **Findet freie virtuelle Tasten** der Schnittstelle des Aktors (ohne Verknüpfung, ohne Namen, ohne fremde
  Markierung; von Taste 50 abwärts) und **legt die Direktverknüpfungen an**: je Aktor zwei Tasten, eine für
  *ein / heller*, eine für *aus / dunkler* (bei Rollläden *hoch* und *runter*). Die Verknüpfungsparameter werden nach
  den Profilen der WebUI geschrieben; nur was sich vom Vorhandenen unterscheidet, wird gesendet.
- **Leitet die Tastendrücke weiter:** kurz drücken schaltet, lang drücken dimmt Schritt für Schritt, solange die Taste
  gehalten wird. Jeder weitergeleitete Druck ist ein Schritt des Aktors; das Addon spiegelt die Wiederholungen des
  Tasters, höchstens einen Druck je 300 ms, und verwirft, was schneller kommt (schnellere Drücke stauen sich im
  Schnittstellenprozess, der Aktor schießt über).
- **Ein- oder Zwei-Tasten-Modus:** zwei Tasten (eine ein/heller, eine aus/dunkler) oder eine Taste (kurz schaltet um,
  lang dimmt abwechselnd hoch und runter; am Ende entscheidet der zuletzt gemeldete Pegel).
- **Ohne Taster:** die Verknüpfung kann auch ohne Taster angelegt werden - dann drückt eine externe Logik (Node-RED,
  she, Home Assistant, ein Skript) die virtuellen Tasten per RPC, siehe unten.
- **Vorhandene Verknüpfungen** von virtuellen Tasten zu Dimmern und Rollläden, die das Addon nicht angelegt hat, werden
  gezeigt und können übernommen werden (nur markiert, nichts wird umgeschrieben). Die Zuordnung der Taster lässt sich
  aus der `Mapping`-Funktion eines Node-RED-Flows einlesen.
- **HmIP-Taster melden ihre Drücke erst, wenn eine Verwendung registriert ist** (`reportValueUsage`); auf einer
  CCU erledigt das die Logikschicht für Programme, auf openccu-lite dieses Addon für seine Taster - beim Anlegen
  registriert, beim Entfernen wieder freigegeben (abschaltbar, falls ein anderes Programm dieselbe Taste braucht).
- **Schrittweite je Verknüpfung** (`LONG_DIM_STEP`, Standard 5 %; bei Rollläden die Fahrzeit je Druck), *ein* auf
  100 % oder auf den letzten Pegel, Grenzen des Dimmbereichs.
- **Sicher schreiben:** jede Änderung wird erst als Plan gezeigt (welche Verknüpfungen angelegt, welche Parameter
  geändert, welche Tasten benannt werden) und dann ausgeführt; das Ergebnis wird zurückgelesen. Ein HmIP-Gerät, das
  gerade nicht erreichbar ist, nimmt die Verknüpfung später an (`CONFIG_PENDING`); das Addon zeigt *wartet auf Gerät*
  und prüft weiter. Es fasst nur Tasten an, die es selbst markiert hat, und nur Verknüpfungen, die es angelegt oder
  übernommen hat.

Unterstützt werden Dimmer und Rollladenaktoren der drei Familien: BidCos-RF (`DIMMER`, `BLIND`), BidCos-Wired
(`HMW_DIMMER`, `HMW_BLIND`) und HmIP/HmIPW (`DIMMER_VIRTUAL_RECEIVER`, `BLIND_VIRTUAL_RECEIVER`,
`SHUTTER_VIRTUAL_RECEIVER`). Taster und Aktor müssen auf **verschiedenen** Schnittstellen liegen - auf derselben ist
die Direktverknüpfung besser.

## Installation

Das Paket `longpressdim-<arch>-<version>.tar.gz` von der Releases-Seite über die Addons-Seite des Systems
installieren. Das Addon braucht die Rechte `rpc:read`, `rpc:operate`, `rpc:configure`, `meta:read` und `meta:write`:
es liest Geräte, Verknüpfungen und den Ereignisstrom, legt Verknüpfungen an, drückt virtuelle Tasten und benennt sie.
Es kontaktiert nichts außerhalb des Systems.

Die App ist unter `/addons/longpressdim/` erreichbar, die Einstellungsseite (Weiterleitung, *alle Verknüpfungen
entfernen*) über den Einstellungen-Knopf des Addons.

## Auf einer CCU3 oder OpenCCU

Dasselbe Paket lässt sich als klassisches Addon installieren (*Einstellungen → Systemsteuerung → Zusatzsoftware*,
oder `/bin/install_addon` auf OpenCCU). Dort spricht der Dienst direkt mit den Schnittstellenprozessen - per
XML-RPC auf dem Loopback, mit einem Rückkanal auf Port 8294 für die Tastendrücke - und nimmt Namen und Räume aus der
ReGaHSS; die Markierung der verwendeten virtuellen Tasten liegt in der eigenen Datei des Addons
(`var/markers.json`), ihr Name wird in der ReGaHSS gesetzt. Der *Einstellungen*-Knopf des Addons öffnet die
Einstellungsseite, von dort aus die Anwendung. Auf einer CCU3 mit der Firmware von eQ-3 startet der Dienst mit dem
Neustart nach der Installation.

## Deinstallation

Eine Deinstallation lässt die Direktverknüpfungen auf den Aktoren und die Namen der virtuellen Tasten stehen - die
Dimmer funktionieren mit jeder anderen Weiterleitung weiter, und ein stilles Löschen vieler Verknüpfungen zur
Deinstallationszeit wäre der riskantere Weg. Wer alles los sein will, nimmt vorher *Alle Verknüpfungen entfernen* auf
der Einstellungsseite. Die Einstellungen (`etc/`) und die Verknüpfungsliste (`var/`) bleiben für eine Neuinstallation
unter `/usr/local/addons/longpressdim/`.

## Virtuelle Tasten von außen drücken

Für eine Verknüpfung ohne Taster (oder zusätzlich zu ihren Tastern) drückt eine externe Logik die beiden virtuellen
Tasten der Verknüpfung - die App zeigt sie je Verknüpfung an. Die Regel: **kurz** = `PRESS_SHORT` einmal; **halten** =
`PRESS_LONG` alle ~300 ms auf der Taste der Richtung, solange gehalten wird, nichts beim Loslassen. Jeder Druck ist ein
Schritt; schneller als alle 300 ms staut sich im Schnittstellenprozess.

Über lite-rpc (ein API-Token mit `rpc:operate`):

```sh
curl -u :<token> -X POST https://<system>/api/rpc/v1/json/BidCos-Wired \
  -d '{"jsonrpc":"2.0","method":"setValue","params":["BidCoS-Wir:48","PRESS_LONG",true],"id":1}'
```

Über XML-RPC (wenn der klassische RPC-Zugang eingeschaltet ist): `setValue("BidCoS-Wir:48", "PRESS_LONG", true)` auf
Port 2000 (Wired), 2001 (BidCos-RF) oder 2010 (HmIP). Über hm2mqtt.js: `<prefix>/set/<Name der Taste>/PRESS_LONG` mit
`true`. In Node-RED: ein `ccu-value`-Knoten mit dem Datenpunkt `PRESS_LONG`.

## Entwicklung

Go-Backend auf [ccu-addon-kit](https://github.com/hobbyquaker/ccu-addon-kit) (`go test -race ./...`), die Oberfläche
Svelte 5 auf [occulite-ui](https://github.com/hobbyquaker/occulite-ui) (`cd ui && npm ci && npm run check && npm test
&& npm run build`), die Pakete mit `VERSION=x.y.z scripts/build.sh`. Die API ist in [docs/api.md](docs/api.md)
beschrieben und in `docs/openapi.json` spezifiziert.

Die Profiltabelle (`internal/profiles`) ist die eigene des Addons: je Familie die Verknüpfungsparameter, die aus den
Profilen der WebUI (*ein/heller*, *aus/dunkler*, *umschalten*) abgeleitet sind.

## Lizenz

MIT

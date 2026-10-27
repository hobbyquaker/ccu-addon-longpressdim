// The settings page's strings, as the kit frame takes them - {key: {de, en}}.
const strings: Record<string, {de: string; en: string}> = {
    cancel: {de: 'Abbrechen', en: 'Cancel'},
    'relay.title': {de: 'Weiterleitung', en: 'Relay'},
    'relay.intro': {
        de: 'Jeder weitergeleitete lange Tastendruck ist ein Schritt des Aktors. Das Addon leitet die Wiederholungen des Tasters weiter, höchstens einen Druck je Intervall; was schneller kommt, wird verworfen (schnellere Drücke stauen sich im Schnittstellenprozess und der Aktor schießt über).',
        en: 'Every relayed long press is one step of the actuator. The addon mirrors the sender\'s repeats, at most one press per interval; what comes faster is dropped (faster presses queue in the interface process and the actuator overshoots).',
    },
    'relay.interval': {de: 'Intervall zwischen zwei Schritten (ms)', en: 'Interval between two steps (ms)'},
    'relay.interval.hint': {de: '100 bis 2000, Standard 300. Je Verknüpfung überschreibbar.', en: '100 to 2000, default 300. Each binding may override it.'},
    'relay.holdTimeout': {de: 'Halten gilt als beendet nach (ms)', en: 'A hold counts as ended after (ms)'},
    'relay.holdTimeout.hint': {de: 'Ohne Loslassen-Meldung des Tasters: so lange nach der letzten Wiederholung. 300 bis 5000, Standard 1000.', en: 'Without a release report from the sender: this long after the last repeat. 300 to 5000, default 1000.'},
    'relay.logPresses': {de: 'Jeden weitergeleiteten Druck protokollieren', en: 'Log every relayed press'},
    'relay.save': {de: 'Speichern', en: 'Save'},
    'relay.saved': {de: 'Einstellungen gespeichert.', en: 'Settings saved.'},
    'relay.admin': {de: 'Nur Administratoren ändern die Einstellungen.', en: 'Only administrators change the settings.'},
    'removeAll.title': {de: 'Alle Verknüpfungen entfernen', en: 'Remove all bindings'},
    'removeAll.intro': {
        de: 'Eine Deinstallation des Addons lässt die Direktverknüpfungen auf den Aktoren und die Namen der virtuellen Tasten stehen. Hier werden alle Verknüpfungen des Addons entfernt: die Direktverknüpfungen gelöscht, die Tasten freigegeben.',
        en: 'Uninstalling the addon leaves the direct links on the actuators and the virtual keys\' names in place. This removes every binding of the addon: the direct links are deleted, the keys released.',
    },
    'removeAll.count': {de: '{count} Verknüpfungen vorhanden.', en: '{count} bindings present.'},
    'removeAll.forget': {de: 'Nur vergessen: die Direktverknüpfungen bleiben', en: 'Forget only: the direct links stay'},
    'removeAll.button': {de: 'Alle Verknüpfungen entfernen', en: 'Remove all bindings'},
    'removeAll.question': {de: 'Wirklich alle {count} Verknüpfungen entfernen?', en: 'Really remove all {count} bindings?'},
    'removeAll.confirm': {de: 'Ja, alle entfernen', en: 'Yes, remove all'},
    'removeAll.done': {de: '{count} Verknüpfungen entfernt.', en: '{count} bindings removed.'},
    'removeAll.error': {de: 'Fehler: {message}', en: 'Error: {message}'},
};

export default strings;

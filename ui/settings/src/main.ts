// The settings page of the longpressdim addon: the ccu-addon-kit frame (service, log, about - on
// a CCU; inside openccu-lite's shell the system has its own pages for those) with the addon's own
// tab, the relay's settings and "remove all bindings". Built by Vite into settings/dist, which
// scripts/build.sh hands to `ccu-addon-kit vendor --frame`.
import 'occulite-ui/app.css';
import {defineTabs, mountFrame} from 'ccu-addon-kit/frame';

import strings from './i18n.js';
import Relay from './tabs/Relay.svelte';

const tabs = defineTabs([{id: 'relay', label: {de: 'Weiterleitung', en: 'Relay'}, component: Relay}]);

void mountFrame(document.getElementById('app')!, {tabs, strings});

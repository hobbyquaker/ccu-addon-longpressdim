<script lang="ts">
    // The app: the bindings (with their editor), the links the addon found but does not manage,
    // the virtual keys per interface, the import of a Node-RED mapping. The relay's settings and
    // the service live on the addon's settings page (the ccu-addon-kit frame), not here.
    import {Notices, Tabs, ThemeSwitch} from 'occulite-ui';
    import {i18n, t} from 'occulite-ui/i18n';
    import {NoticesStore} from 'occulite-ui/notices';
    import {cycleTheme, theme} from 'occulite-ui/theme';

    import BindingsPage from './BindingsPage.svelte';
    import Editor from './Editor.svelte';
    import ForeignPage from './ForeignPage.svelte';
    import ImportPage from './ImportPage.svelte';
    import KeysPage from './KeysPage.svelte';
    import {get} from './api.js';
    import type {Health, View} from './model.js';

    const notices = new NoticesStore();
    let tab = $state('bindings');
    let health = $state<Health | undefined>();
    let editorOpen = $state(false);
    let editing = $state<View | undefined>();
    let reload = $state(0);

    async function loadHealth() {
        try {
            health = await get('api/v1/health');
        } catch {
            health = undefined;
        }
    }

    $effect(() => {
        loadHealth();
        const id = setInterval(loadHealth, 30_000);
        return () => clearInterval(id);
    });

    function openEditor(b: View | undefined) {
        editing = b;
        editorOpen = true;
    }

    function changed() {
        loadHealth();
        reload++;
    }

    const tabs = $derived([
        {id: 'bindings', label: t('Bindings'), ...(health?.service.changed ? {badge: health.service.changed} : {})},
        {id: 'foreign', label: t('Found links')},
        {id: 'keys', label: t('Keys')},
        {id: 'import', label: t('Import')},
    ]);
</script>

<div class="lpd-app" lang={i18n.language}>
    <header class="lpd-header">
        <h1>{t('Long press dim')}</h1>
        <Tabs {tabs} bind:active={tab} label={t('Long press dim')} />
        <span class="lpd-spacer"></span>
        {#if !theme.followsShell}
            <ThemeSwitch theme={theme.choice} onclick={cycleTheme} />
        {/if}
    </header>
    {#if health && !health.service.stream}
        <div class="lpd-banner" role="status">{t('The event stream is not connected ({detail}).', {detail: health.service.stream_detail || '…'})}</div>
    {/if}
    <main class="lpd-page">
        {#if tab === 'bindings'}
            {#key reload}
                <BindingsPage {notices} onchanged={loadHealth} onopen={openEditor} />
            {/key}
        {:else if tab === 'foreign'}
            <ForeignPage {notices} onchanged={changed} />
        {:else if tab === 'keys'}
            <KeysPage {notices} />
        {:else}
            <ImportPage {notices} onchanged={changed} />
        {/if}
    </main>
    <Editor open={editorOpen} binding={editing} {notices} onclose={() => (editorOpen = false)} onchanged={changed} />
    <Notices notices={notices.visible} ondismiss={(id) => notices.dismiss(id)} />
</div>

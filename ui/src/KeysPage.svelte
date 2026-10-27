<script lang="ts">
    // The 50 virtual keys of an interface: their links, names, markers, and which are free.
    import {DataTable} from 'occulite-ui';
    import {t} from 'occulite-ui/i18n';
    import type {NoticesStore} from 'occulite-ui/notices';
    import type {DataTableColumn} from 'occulite-ui/table';

    import {get} from './api.js';
    import type {Key} from './model.js';

    let {notices}: {notices: NoticesStore} = $props();

    let interfaces = $state<{name: string}[]>([]);
    let iface = $state('');
    let keys = $state<Key[]>([]);
    let loading = $state(true);

    async function loadInterfaces() {
        try {
            const r = await get<{interfaces: {name: string}[]}>('api/v1/interfaces');
            interfaces = r.interfaces;
            if (!iface && interfaces[0]) iface = interfaces[0].name;
        } catch (e) {
            notices.push('error', (e as Error).message);
        }
    }

    async function load(name: string) {
        if (!name) return;
        loading = true;
        try {
            const r = await get<{keys: Key[]}>(`api/v1/keys/${encodeURIComponent(name)}`);
            keys = r.keys;
        } catch (e) {
            notices.push('error', (e as Error).message);
        } finally {
            loading = false;
        }
    }

    $effect(() => {
        loadInterfaces();
    });
    $effect(() => {
        load(iface);
    });

    function linksText(k: Key): string {
        return (k.links ?? []).map((l) => `${l.SENDER} → ${l.RECEIVER}${l.NAME ? ` (${l.NAME})` : ''}`).join(', ');
    }

    const columns = $derived<DataTableColumn<Key>[]>([
        {key: 'n', label: '#', width: 50, fixed: true, align: 'right', value: (k) => k.n},
        {key: 'address', label: t('Address'), width: 130, mono: true},
        {key: 'name', label: t('Name'), width: 200, value: (k) => k.name ?? ''},
        {key: 'links', label: t('Links'), width: 320, value: linksText},
        {key: 'marker', label: t('Bindings'), width: 180, value: (k) => (k.marker ? t('keys.marker', {binding: k.marker.binding, role: t('role.' + k.marker.role)}) : k.foreign ? t('keys.foreign') : '')},
        {key: 'free', label: t('Free'), width: 90, fixed: true, align: 'center', value: (k) => (k.free ? t('keys.free.yes') : t('keys.free.no'))},
    ]);
</script>

<p class="lpd-muted">{t('keys.intro')}</p>
<div class="lpd-toolbar">
    <label>{t('Interface')} <select bind:value={iface}>{#each interfaces as i (i.name)}<option value={i.name}>{i.name}</option>{/each}</select></label>
</div>
{#if loading}
    <p class="lpd-empty">{t('Loading…')}</p>
{:else}
    <div class="lpd-table">
        <DataTable rows={keys} {columns} getId={(k) => k.address} columnFilterRow={false} caption={t('Keys')} countText={t('{count} keys', {count: keys.length}, keys.length)} tableId="longpressdim-keys" testId="keys">
            {#snippet cell(row, column)}
                {#if column.key === 'free'}
                    <span class="lpd-pill small {row.free ? 'active' : ''}">{row.free ? t('keys.free.yes') : t('keys.free.no')}</span>
                {:else}
                    <span class="lpd-truncate" title={String(column.value ? (column.value(row) ?? '') : '')}>{column.value ? String(column.value(row) ?? '') : String((row as unknown as Record<string, unknown>)[column.key] ?? '')}</span>
                {/if}
            {/snippet}
        </DataTable>
    </div>
{/if}

<script lang="ts">
    // The bindings: one row per actuator with its keys, senders, status and the last relayed
    // hold; live through the service's event stream.
    import {DataTable} from 'occulite-ui';
    import {i18n, t} from 'occulite-ui/i18n';
    import type {NoticesStore} from 'occulite-ui/notices';
    import type {DataTableColumn} from 'occulite-ui/table';

    import RemoveDialog from './RemoveDialog.svelte';
    import {get, post, stream} from './api.js';
    import {formatTime, keyLabel, type Result, type View} from './model.js';

    let {notices, onchanged, onopen}: {notices: NoticesStore; onchanged?: () => void; onopen?: (b: View | undefined) => void} = $props();

    let rows = $state<View[]>([]);
    let loading = $state(true);
    let removing = $state<View | undefined>();
    let checking = $state(false);
    const lang = $derived(i18n.language);

    async function load() {
        try {
            const r = await get<{bindings: View[]}>('api/v1/bindings');
            rows = r.bindings;
        } catch (e) {
            notices.push('error', (e as Error).message);
        } finally {
            loading = false;
        }
    }

    $effect(() => {
        load();
        let timer: ReturnType<typeof setTimeout> | undefined;
        const s = stream((type) => {
            if (type === 'press' || type === 'level') {
                // the activity changed: reload a little later, once per burst
                if (!timer) timer = setTimeout(() => ((timer = undefined), load()), 1500);
            } else {
                load();
                onchanged?.();
            }
        });
        return () => {
            s.close();
            if (timer) clearTimeout(timer);
        };
    });

    async function check() {
        checking = true;
        try {
            const r = await post<{bindings: View[]}>('api/v1/check');
            rows = r.bindings;
            onchanged?.();
        } catch (e) {
            notices.push('error', (e as Error).message);
        } finally {
            checking = false;
        }
    }

    async function reapply(b: View) {
        try {
            const r = await post<Result>(`api/v1/bindings/${encodeURIComponent(b.id)}/reapply`);
            notices.push('info', r.pending ? t('binding.pending') : t('binding.applied'));
            load();
            onchanged?.();
        } catch (e) {
            notices.push('error', (e as Error).message);
        }
    }

    function senders(b: View): string {
        if (!b.sender_views.length) return t('senders.none');
        return b.sender_views.map((s) => `${s.name || s.address} (${t('role.' + s.role)})`).join(', ');
    }

    function hold(b: View): string {
        const a = b.activity;
        if (!a.last_hold || a.last_hold.startsWith('0001')) return t('hold.never');
        return `${formatTime(a.last_hold, lang)}: ${t('hold.steps', {steps: a.steps, dir: t('role.' + (a.last_direction || 'up'))})}`;
    }

    const columns = $derived<DataTableColumn<View>[]>([
        {key: 'name', label: t('Actuator'), width: 200, value: (b) => b.target_name || b.target.address},
        {key: 'room', label: t('Room'), width: 110, value: (b) => (b.target_rooms ?? []).join(', ')},
        {key: 'interface', label: t('Interface'), width: 100, value: (b) => b.target.interface},
        {key: 'kind', label: t('kind.dimmer'), width: 90, value: (b) => t('kind.' + b.kind)},
        {key: 'mode', label: t('Mode'), width: 100, value: (b) => t('mode.' + b.mode)},
        {key: 'senders', label: t('Senders'), width: 220, value: senders},
        {key: 'keys', label: t('Keys'), width: 110, fixed: true, value: (b) => `${keyLabel(b.up, 'up')} ${keyLabel(b.down, 'down')}`, sortable: false},
        {key: 'status', label: t('Status'), width: 150, fixed: true, value: (b) => b.status.state, align: 'center'},
        {key: 'hold', label: t('Last hold'), width: 170, value: hold, filterable: false},
        {key: 'actions', label: '', width: 150, fixed: true, sortable: false, filterable: false},
    ]);

    function statusTitle(b: View): string {
        const parts = [t('status.' + b.status.state)];
        if (b.status.detail) parts.push(b.status.detail);
        if (b.status.differs) for (const [k, v] of Object.entries(b.status.differs)) parts.push(`${k}: ${Object.keys(v).join(', ')}`);
        return parts.join(' - ');
    }
</script>

{#if loading}
    <p class="lpd-empty">{t('Loading…')}</p>
{:else}
    <div class="lpd-table">
        <DataTable
            {rows}
            {columns}
            getId={(b) => b.id}
            columnFilterRow={false}
            caption={t('Bindings')}
            emptyText={t('No bindings yet.')}
            countText={t('{count} bindings', {count: rows.length}, rows.length)}
            showingText={(shown, total) => t('Showing {shown} of {total}', {shown, total})}
            onactivate={(b) => onopen?.(b)}
            tableId="longpressdim-bindings"
            testId="bindings"
        >
            {#snippet toolbar()}
                {#if onopen}
                    <button type="button" class="primary" onclick={() => onopen?.(undefined)}>{t('New binding')}</button>
                {/if}
                <button type="button" onclick={check} disabled={checking}>{t('Check now')}</button>
            {/snippet}
            {#snippet cell(row, column)}
                {#if column.key === 'name'}
                    {#if onopen}
                        <button type="button" class="lpd-link lpd-truncate" title={t('binding.open')} onclick={() => onopen?.(row)}>{row.target_name || row.target.address}</button>
                    {:else}
                        <span class="lpd-truncate" title={row.target.address}>{row.target_name || row.target.address}</span>
                    {/if}
                    {#if row.adopted}<span class="lpd-chip" title={t('adopted')}>{t('adopted')}</span>{/if}
                {:else if column.key === 'keys'}
                    <span class="lpd-mono" title="{row.up} / {row.down}">{keyLabel(row.up, 'up')} {keyLabel(row.down, 'down')}</span>
                {:else if column.key === 'status'}
                    <span class="lpd-pill {row.status.state}" title={statusTitle(row)}>{t('status.' + row.status.state)}</span>
                {:else if column.key === 'actions'}
                    <div class="lpd-row-actions">
                        {#if row.status.state === 'changed' || row.status.state === 'missing-link' || row.status.state === 'error'}
                            <button type="button" class="lpd-cell-button" title={t('binding.reapply')} onclick={() => reapply(row)}>{t('Apply')}</button>
                        {/if}
                        <button type="button" class="lpd-cell-button" onclick={() => (removing = row)}>{t('Remove')}</button>
                    </div>
                {:else}
                    {column.value ? String(column.value(row) ?? '') : ''}
                {/if}
            {/snippet}
        </DataTable>
    </div>
{/if}

<RemoveDialog
    binding={removing}
    onclose={() => (removing = undefined)}
    ondone={() => {
        notices.push('info', t('binding.removed'));
        load();
        onchanged?.();
    }}
/>

<script lang="ts">
    // "Present, not managed": links from virtual keys to dimmers and blinds the addon did not
    // create, grouped by actuator, each key classified by its link parameters; a pair (one up,
    // one down) is adopted - marked - from here.
    import {Dialog} from 'occulite-ui';
    import {t} from 'occulite-ui/i18n';
    import type {NoticesStore} from 'occulite-ui/notices';

    import PlanView from './PlanView.svelte';
    import {get, post} from './api.js';
    import {keyNumber, percent, type ForeignLink, type Result} from './model.js';

    let {notices, onchanged}: {notices: NoticesStore; onchanged?: () => void} = $props();

    let links = $state<ForeignLink[]>([]);
    let loading = $state(true);
    let adopting = $state<{receiver: ForeignLink['Receiver']; iface: string; keys: ForeignLink[]} | undefined>();
    let up = $state('');
    let down = $state('');
    let busy = $state(false);
    let result = $state<Result | undefined>();
    let error = $state('');

    async function load() {
        loading = true;
        try {
            const r = await get<{links: ForeignLink[]}>('api/v1/foreign');
            links = r.links;
        } catch (e) {
            notices.push('error', (e as Error).message);
        } finally {
            loading = false;
        }
    }
    $effect(() => {
        load();
    });

    const groups = $derived.by(() => {
        const m = new Map<string, {receiver: ForeignLink['Receiver']; iface: string; keys: ForeignLink[]}>();
        for (const l of links) {
            const k = `${l.interface}|${l.Receiver.address}`;
            if (!m.has(k)) m.set(k, {receiver: l.Receiver, iface: l.interface, keys: []});
            m.get(k)!.keys.push(l);
        }
        return [...m.values()];
    });

    function startAdopt(g: {receiver: ForeignLink['Receiver']; iface: string; keys: ForeignLink[]}) {
        adopting = g;
        result = undefined;
        error = '';
        up = g.keys.find((k) => k.profile?.long === 'up')?.key ?? '';
        down = g.keys.find((k) => k.profile?.long === 'down')?.key ?? '';
    }

    async function adopt() {
        if (!adopting) return;
        busy = true;
        error = '';
        try {
            result = await post<Result>('api/v1/foreign/adopt', {interface: adopting.iface, target: adopting.receiver.address, up, down});
            notices.push('info', t('foreign.adopted'));
            onchanged?.();
            load();
        } catch (e) {
            error = (e as Error).message;
        } finally {
            busy = false;
        }
    }

    function stepText(l: ForeignLink): string {
        if (!l.profile?.step) return '–';
        return l.Receiver.kind === 'blind' ? `${l.profile.step} s` : percent(l.profile.step);
    }
</script>

<p class="lpd-muted">{t('foreign.intro')}</p>
{#if loading}
    <p class="lpd-empty">{t('Loading…')}</p>
{:else if groups.length === 0}
    <p class="lpd-empty">{t('foreign.none')}</p>
{:else}
    {#each groups as g (g.iface + g.receiver.address)}
        <section class="lpd-card">
            <h2>{g.receiver.name || g.receiver.address} <span class="lpd-muted lpd-mono">{g.receiver.address} · {g.iface} · {t('kind.' + (g.receiver.kind ?? 'dimmer'))}</span></h2>
            <table class="lpd-plain">
                <thead><tr><th>{t('Key')}</th><th>{t('Name')}</th><th>{t('foreign.long')}</th><th>{t('foreign.short')}</th><th>{t('foreign.step')}</th></tr></thead>
                <tbody>
                    {#each g.keys as l (l.key)}
                        <tr>
                            <td class="lpd-mono">{keyNumber(l.key)}</td>
                            <td>{l.key_name || l.link.NAME || '–'}</td>
                            {#if l.profile}
                                <td>{t('long.' + l.profile.long)}</td>
                                <td>{t('short.' + l.profile.short)}</td>
                                <td>{stepText(l)}</td>
                            {:else}
                                <td colspan="3" class="lpd-warn">{t('foreign.unreadable', {error: l.error ?? ''})}</td>
                            {/if}
                        </tr>
                    {/each}
                </tbody>
            </table>
            <p><button type="button" class="primary" onclick={() => startAdopt(g)} disabled={g.keys.length < 2}>{t('foreign.adopt')}</button></p>
        </section>
    {/each}
{/if}

<Dialog open={adopting !== undefined} title={t('foreign.adopt.title', {name: adopting?.receiver.name || adopting?.receiver.address || ''})} width="30rem" closable={!busy} onclose={() => (adopting = undefined)} testId="adopt">
    {#if adopting}
        <p class="lpd-muted">{t('foreign.adopt.hint')}</p>
        <div class="lpd-form">
            <label class="lpd-label" for="lpd-adopt-up">{t('foreign.adopt.up')}</label>
            <select id="lpd-adopt-up" bind:value={up} disabled={busy || !!result}>
                {#each adopting.keys as l (l.key)}<option value={l.key}>{keyNumber(l.key)} {l.key_name ? `(${l.key_name})` : ''} - {l.profile ? t('long.' + l.profile.long) : '?'}</option>{/each}
            </select>
            <label class="lpd-label" for="lpd-adopt-down">{t('foreign.adopt.down')}</label>
            <select id="lpd-adopt-down" bind:value={down} disabled={busy || !!result}>
                {#each adopting.keys as l (l.key)}<option value={l.key}>{keyNumber(l.key)} {l.key_name ? `(${l.key_name})` : ''} - {l.profile ? t('long.' + l.profile.long) : '?'}</option>{/each}
            </select>
        </div>
        {#if result}<PlanView {result} />{/if}
        {#if error}<p class="lpd-danger">{t('error.generic', {message: error})}</p>{/if}
    {/if}
    {#snippet buttons()}
        <div class="lpd-dialog-buttons">
            <button type="button" onclick={() => (adopting = undefined)} disabled={busy}>{result ? t('Close') : t('Cancel')}</button>
            {#if !result}<button type="button" class="primary" onclick={adopt} disabled={busy || !up || !down || up === down}>{t('foreign.adopt')}</button>{/if}
        </div>
    {/snippet}
</Dialog>

<style>
    .lpd-plain {
        border-collapse: collapse;
        width: 100%;
    }
    .lpd-plain th,
    .lpd-plain td {
        text-align: left;
        padding: 2px 8px 2px 0;
        border-bottom: 1px solid var(--hmm-border, #8884);
    }
    .lpd-plain th {
        font-weight: 600;
        opacity: 0.8;
    }
</style>

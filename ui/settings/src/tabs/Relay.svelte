<script lang="ts">
    // The settings page's own tab: the relay's timing and "remove all bindings". It talks to
    // the addon's Go backend at /addons/longpressdim/api/v1/ - the same origin, the session the
    // platform hands over - never to a CGI of its own.
    import {Dialog} from 'occulite-ui';
    import {getKit, KitError, KitLoading, REQUEST_HEADER} from 'ccu-addon-kit/frame';

    let {active}: {active: boolean; path: string} = $props();
    const kit = getKit();

    const API = '/addons/longpressdim/api/v1/';

    interface Settings {
        step_interval_ms: number;
        hold_timeout_ms: number;
        log_presses: boolean;
    }

    async function call<T>(path: string, method: 'GET' | 'PUT' | 'POST', body?: unknown): Promise<T> {
        const sid = kit.platform.kind === 'openccu-lite' ? '' : (new URLSearchParams(location.search).get('sid') ?? '');
        const url = API + path + (sid ? (path.includes('?') ? '&' : '?') + 'sid=' + encodeURIComponent(sid) : '');
        const res = await fetch(url, {
            method,
            headers: {Accept: 'application/json', ...(method === 'GET' ? {} : {'Content-Type': 'application/json', [REQUEST_HEADER]: '1'})},
            credentials: 'same-origin',
            cache: 'no-store',
            body: body === undefined ? undefined : JSON.stringify(body),
        });
        const text = await res.text();
        let parsed: unknown = text;
        try {
            parsed = text ? JSON.parse(text) : {};
        } catch {
            /* not JSON */
        }
        if (!res.ok) {
            const b = parsed as {message?: string; error?: string};
            throw new Error(b?.message ?? b?.error ?? res.statusText);
        }
        return parsed as T;
    }

    let s = $state<Settings>({step_interval_ms: 300, hold_timeout_ms: 1000, log_presses: false});
    let count = $state(0);
    let loaded = $state(false);
    let error = $state<unknown>(null);
    let saving = $state(false);
    let forget = $state(false);
    let asking = $state(false);
    let removing = $state(false);
    const admin = $derived(kit.session.level === 'admin' || kit.platform.kind !== 'openccu-lite');

    async function load() {
        try {
            s = await call<Settings>('settings', 'GET');
            const b = await call<{bindings: unknown[]}>('bindings', 'GET');
            count = b.bindings.length;
            loaded = true;
            error = null;
        } catch (e) {
            error = e;
        }
    }

    $effect(() => {
        if (active) load();
    });

    async function save() {
        saving = true;
        try {
            s = await call<Settings>('settings', 'PUT', s);
            kit.notify('success', kit.t('relay.saved'));
        } catch (e) {
            kit.notify('error', (e as Error).message);
        } finally {
            saving = false;
        }
    }

    async function removeAll() {
        removing = true;
        try {
            const r = await call<{removed: number; error?: string}>(`bindings/remove-all?forget=${forget ? 1 : 0}`, 'POST');
            if (r.error) kit.notify('error', kit.t('removeAll.error', {message: r.error}));
            else kit.notify('success', kit.t('removeAll.done', {count: r.removed}));
            asking = false;
            await load();
        } catch (e) {
            kit.notify('error', kit.t('removeAll.error', {message: (e as Error).message}));
        } finally {
            removing = false;
        }
    }
</script>

{#if error}
    <KitError {error} retry={load} />
{:else if !loaded}
    <KitLoading />
{:else}
    <section class="lpd-card">
        <h2>{kit.t('relay.title')}</h2>
        <p>{kit.t('relay.intro')}</p>
        <div class="lpd-form">
            <label class="lpd-label" for="lpd-interval">{kit.t('relay.interval')}</label>
            <div><input id="lpd-interval" type="number" min="100" max="2000" step="10" bind:value={s.step_interval_ms} disabled={!admin || saving} /><div class="lpd-muted">{kit.t('relay.interval.hint')}</div></div>
            <label class="lpd-label" for="lpd-hold">{kit.t('relay.holdTimeout')}</label>
            <div><input id="lpd-hold" type="number" min="300" max="5000" step="100" bind:value={s.hold_timeout_ms} disabled={!admin || saving} /><div class="lpd-muted">{kit.t('relay.holdTimeout.hint')}</div></div>
            <div class="lpd-full"><label><input type="checkbox" bind:checked={s.log_presses} disabled={!admin || saving} /> {kit.t('relay.logPresses')}</label></div>
            <div class="lpd-full">
                <button type="button" class="primary" onclick={save} disabled={!admin || saving}>{kit.t('relay.save')}</button>
                {#if !admin}<span class="lpd-muted"> {kit.t('relay.admin')}</span>{/if}
            </div>
        </div>
    </section>
    <section class="lpd-card">
        <h2>{kit.t('removeAll.title')}</h2>
        <p>{kit.t('removeAll.intro')}</p>
        <p>{kit.t('removeAll.count', {count})}</p>
        <label><input type="checkbox" bind:checked={forget} disabled={!admin} /> {kit.t('removeAll.forget')}</label>
        <p><button type="button" class="danger" onclick={() => (asking = true)} disabled={!admin || count === 0}>{kit.t('removeAll.button')}</button></p>
    </section>
{/if}

<Dialog open={asking} title={kit.t('removeAll.title')} width="26rem" closable={!removing} onclose={() => (asking = false)}>
    <p>{kit.t('removeAll.question', {count})}</p>
    {#snippet buttons()}
        <div class="lpd-dialog-buttons">
            <button type="button" onclick={() => (asking = false)} disabled={removing}>{kit.t('cancel')}</button>
            <button type="button" class="primary danger" onclick={removeAll} disabled={removing}>{kit.t('removeAll.confirm')}</button>
        </div>
    {/snippet}
</Dialog>

<style>
    .lpd-card {
        max-width: 44rem;
        padding: 12px 14px;
        margin-bottom: 14px;
        border: 1px solid var(--hmm-border, #8884);
        border-radius: 8px;
    }
    .lpd-card h2 {
        font-size: 0.95rem;
        margin: 0 0 8px;
    }
    .lpd-form {
        display: grid;
        grid-template-columns: max-content 1fr;
        gap: 8px 12px;
        align-items: start;
    }
    .lpd-form input[type='number'] {
        width: 8em;
    }
    .lpd-full {
        grid-column: 1 / -1;
    }
    .lpd-muted {
        opacity: 0.7;
        font-size: 0.9em;
    }
    .lpd-dialog-buttons {
        display: flex;
        gap: 8px;
        justify-content: flex-end;
    }
</style>

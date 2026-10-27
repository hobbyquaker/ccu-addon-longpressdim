<script lang="ts">
    // The binding editor: the actuator, the mode, the senders (picked, or "press the key now"),
    // the keys, the advanced options, the dry run and the write; for an existing binding the
    // test buttons, the usage state of its HmIP senders and the "press from outside" panel.
    import {Dialog, MultiSelect} from 'occulite-ui';
    import type {MultiSelectOption} from 'occulite-ui';
    import {i18n, t} from 'occulite-ui/i18n';
    import type {NoticesStore} from 'occulite-ui/notices';

    import ExternalPanel from './ExternalPanel.svelte';
    import PlanView from './PlanView.svelte';
    import {get, patch, post, stream} from './api.js';
    import {keyLabel, type Channel, type Key, type Mode, type Plan, type Result, type Sender, type SenderRole, type Settings, type View} from './model.js';

    let {open, binding, notices, onclose, onchanged}: {open: boolean; binding: View | undefined; notices: NoticesStore; onclose: () => void; onchanged: () => void} = $props();

    const editing = $derived(binding !== undefined);
    let targets = $state<Channel[]>([]);
    let senders = $state<Channel[]>([]);
    let settings = $state<Settings | undefined>();
    let loaded = $state(false);

    // the draft
    let target = $state<string[]>([]);
    let mode = $state<Mode>('two-key');
    let draftSenders = $state<{interface: string; address: string; role: SenderRole}[]>([]);
    let keysAuto = $state(true);
    let up = $state<string[]>([]);
    let down = $state<string[]>([]);
    let freeKeys = $state<Key[]>([]);
    let onLevel = $state<'full' | 'last'>('full');
    let stepPct = $state(5);
    let travel = $state(0.5);
    let minPct = $state(0);
    let maxPct = $state(100);
    let interval = $state<number | ''>('');
    let advanced = $state(false);

    let plan = $state<Plan | undefined>();
    let result = $state<Result | undefined>();
    let busy = $state(false);
    let error = $state('');
    let picked = $state<string[]>([]);
    let pickedRole = $state<SenderRole>('up');
    let listening = $state(0);
    let heard = $state<Channel | undefined>();

    const targetChannel = $derived(targets.find((c) => `${c.interface}|${c.address}` === target[0]));
    const kind = $derived(binding?.kind ?? targetChannel?.kind ?? 'dimmer');
    const targetInterface = $derived(binding?.target.interface ?? targetChannel?.interface ?? '');
    const wired = $derived(targetInterface === 'BidCos-Wired');

    function labelOf(c: Channel): string {
        return c.name || c.address;
    }
    function hintOf(c: Channel): string {
        return [c.device, c.interface, ...(c.rooms ?? [])].filter(Boolean).join(' · ');
    }
    const targetOptions = $derived<MultiSelectOption[]>(targets.map((c) => ({value: `${c.interface}|${c.address}`, label: labelOf(c), hint: hintOf(c), description: `${c.address}: ${c.type}`})));
    const senderOptions = $derived<MultiSelectOption[]>(
        senders.map((c) => ({value: `${c.interface}|${c.address}`, label: labelOf(c), hint: hintOf(c), description: `${c.address}: ${c.type}`, disabled: c.interface === targetInterface})),
    );
    const keyOptions = $derived<MultiSelectOption[]>(freeKeys.map((k) => ({value: k.address, label: `${k.n}`, description: k.address})));

    async function load() {
        loaded = false;
        try {
            const [tr, sr, st] = await Promise.all([get<{targets: Channel[]}>('api/v1/targets'), get<{senders: Channel[]}>('api/v1/senders'), get<Settings>('api/v1/settings')]);
            targets = tr.targets;
            senders = sr.senders;
            settings = st;
        } catch (e) {
            error = (e as Error).message;
        } finally {
            loaded = true;
        }
    }

    function reset(b: View | undefined) {
        plan = undefined;
        result = undefined;
        error = '';
        picked = [];
        heard = undefined;
        if (b) {
            target = [`${b.target.interface}|${b.target.address}`];
            mode = b.mode;
            draftSenders = b.senders.map((s) => ({interface: s.interface, address: s.address, role: s.role}));
            keysAuto = false;
            up = [b.up];
            down = [b.down];
            onLevel = (b.options.on_level ?? 1) > 1 ? 'last' : 'full';
            stepPct = Math.round((b.options.step || 0.05) * 1000) / 10;
            travel = b.options.travel || 0.5;
            minPct = Math.round((b.options.min_level ?? 0) * 100);
            maxPct = Math.round((b.options.max_level || 1) * 100);
            interval = b.step_interval_ms || '';
            advanced = false;
        } else {
            target = [];
            mode = 'two-key';
            draftSenders = [];
            keysAuto = true;
            up = [];
            down = [];
            onLevel = 'full';
            stepPct = 5;
            travel = 0.5;
            minPct = 0;
            maxPct = 100;
            interval = '';
            advanced = false;
        }
    }

    $effect(() => {
        if (open) {
            reset(binding);
            load();
        }
    });

    $effect(() => {
        const iface = targetInterface;
        if (!iface || editing) {
            freeKeys = [];
            return;
        }
        get<{keys: Key[]}>(`api/v1/keys/${encodeURIComponent(iface)}`)
            .then((r) => (freeKeys = r.keys.filter((k) => k.free).reverse()))
            .catch(() => (freeKeys = []));
    });

    function draft() {
        const tc = targetChannel;
        const b = binding;
        const options: Record<string, number> = {};
        if (kind === 'dimmer') {
            options.on_level = onLevel === 'last' && !wired ? 1.005 : 1;
            options.step = Math.max(0.01, Math.min(1, stepPct / 100));
            options.min_level = Math.max(0, Math.min(0.99, minPct / 100));
            options.max_level = Math.max(0.01, Math.min(1, maxPct / 100));
        } else {
            options.travel = Math.max(0.1, Math.min(25.5, travel));
        }
        return {
            ...(b ? {id: b.id} : {}),
            target: b ? {interface: b.target.interface, address: b.target.address} : {interface: tc?.interface ?? '', address: tc?.address ?? ''},
            mode,
            ...(keysAuto ? {} : {up: up[0] ?? '', down: down[0] ?? ''}),
            senders: draftSenders,
            options,
            step_interval_ms: interval === '' ? 0 : Number(interval),
        };
    }

    async function doPlan() {
        busy = true;
        error = '';
        result = undefined;
        try {
            plan = await post<Plan>('api/v1/bindings/plan', draft());
        } catch (e) {
            plan = undefined;
            error = (e as Error).message;
        } finally {
            busy = false;
        }
    }

    async function doApply() {
        busy = true;
        error = '';
        try {
            result = await post<Result>('api/v1/bindings', draft());
            notices.push('info', result.pending ? t('binding.pending') : t('binding.applied'));
            onchanged();
        } catch (e) {
            const body = (e as {body?: {result?: Result}}).body;
            if (body?.result) result = body.result;
            error = (e as Error).message;
        } finally {
            busy = false;
        }
    }

    // an existing binding's senders and interval need no link write
    async function doSave() {
        if (!binding) return;
        busy = true;
        error = '';
        try {
            await patch<View>(`api/v1/bindings/${encodeURIComponent(binding.id)}`, {senders: draftSenders, step_interval_ms: interval === '' ? 0 : Number(interval)});
            notices.push('info', t('binding.saved'));
            onchanged();
        } catch (e) {
            error = (e as Error).message;
        } finally {
            busy = false;
        }
    }

    const optionsChanged = $derived.by(() => {
        const b = binding;
        if (!b) return false;
        const d = draft().options;
        const o = b.options;
        return d.on_level !== (o.on_level || 1) || (d.step !== undefined && Math.abs(d.step - (o.step || 0.05)) > 1e-6) || (d.travel !== undefined && Math.abs(d.travel - (o.travel || 0.5)) > 1e-6) || (d.min_level ?? 0) !== (o.min_level ?? 0) || (d.max_level ?? 1) !== (o.max_level || 1) || mode !== b.mode;
    });

    function addPicked() {
        const c = senders.find((s) => `${s.interface}|${s.address}` === picked[0]);
        if (!c) return;
        addSender(c);
        picked = [];
    }

    function addSender(c: Channel) {
        if (draftSenders.some((s) => s.interface === c.interface && s.address === c.address)) return;
        draftSenders = [...draftSenders, {interface: c.interface, address: c.address, role: mode === 'one-key' ? 'both' : pickedRole}];
        plan = undefined;
    }

    function removeSender(i: number) {
        draftSenders = draftSenders.filter((_, j) => j !== i);
        plan = undefined;
    }

    function senderName(s: Sender): string {
        const c = senders.find((x) => x.interface === s.interface && x.address === s.address);
        return c ? `${labelOf(c)} (${c.interface})` : `${s.address} (${s.interface})`;
    }

    let stopListening: (() => void) | undefined;
    function listen() {
        heard = undefined;
        listening = 20;
        const s = stream((type, data) => {
            if (type !== 'press') return;
            const iface = String(data.interface ?? '');
            const address = String(data.address ?? '');
            const c = senders.find((x) => x.interface === iface && x.address === address);
            if (c) {
                heard = c;
                stop();
            }
        });
        const tick = setInterval(() => {
            listening--;
            if (listening <= 0) stop();
        }, 1000);
        const stop = () => {
            s.close();
            clearInterval(tick);
            listening = 0;
            stopListening = undefined;
        };
        stopListening = stop;
    }
    $effect(() => () => stopListening?.());

    async function test(direction: 'up' | 'down', datapoint: 'PRESS_SHORT' | 'PRESS_LONG') {
        if (!binding) return;
        try {
            await post(`api/v1/bindings/${encodeURIComponent(binding.id)}/press`, {direction, datapoint});
            notices.push('info', t('editor.test.sent'));
        } catch (e) {
            notices.push('error', (e as Error).message);
        }
    }

    const title = $derived(binding ? t('editor.edit', {name: binding.target_name || binding.target.address}) : t('editor.new'));
    const hmipSenders = $derived(binding?.sender_views.filter((s) => s.interface === 'HmIP-RF') ?? []);
</script>

<Dialog {open} {title} width="44rem" minWidth={560} top="4vh" closable={!busy} {onclose} testId="binding-editor">
    {#if !loaded}
        <p class="lpd-empty">{t('Loading…')}</p>
    {:else}
        <div class="lpd-form" lang={i18n.language}>
            <span class="lpd-label">{t('editor.target')}</span>
            <div>
                {#if binding}
                    <strong>{binding.target_name || binding.target.address}</strong>
                    <span class="lpd-muted lpd-mono"> {binding.target.address} · {binding.target.interface} · {t('kind.' + binding.kind)}</span>
                    <div class="lpd-muted">{t('editor.target.fixed')}</div>
                {:else}
                    <MultiSelect options={targetOptions} bind:selected={target} multiple={false} placeholder={t('editor.target.pick')} inline={true} onchange={() => (plan = undefined)} testId="target" />
                {/if}
            </div>

            <span class="lpd-label">{t('editor.mode')}</span>
            <div>
                <label><input type="radio" name="mode" value="two-key" bind:group={mode} onchange={() => (plan = undefined)} /> {t('mode.two-key')}</label>
                <span class="lpd-muted"> - {t('mode.two-key.hint')}</span><br />
                <label><input type="radio" name="mode" value="one-key" bind:group={mode} onchange={() => (plan = undefined)} /> {t('mode.one-key')}</label>
                <span class="lpd-muted"> - {t('mode.one-key.hint')}</span>
            </div>

            <span class="lpd-label">{t('editor.senders')}</span>
            <div>
                <div class="lpd-muted">{t('editor.senders.hint')}</div>
                {#if draftSenders.length === 0}
                    <p class="lpd-empty">{t('editor.senders.none')}</p>
                {:else}
                    <ul class="lpd-senders">
                        {#each draftSenders as s, i (s.interface + s.address)}
                            <li>
                                <span class="lpd-truncate">{senderName(s)}</span>
                                <select bind:value={s.role} onchange={() => (plan = undefined)} aria-label={t('editor.senders.role')}>
                                    <option value="up">{t('role.up.long')}</option>
                                    <option value="down">{t('role.down.long')}</option>
                                    <option value="both">{t('role.both.long')}</option>
                                </select>
                                {#if binding}
                                    {@const sv = binding.sender_views.find((x) => x.interface === s.interface && x.address === s.address)}
                                    {#if sv?.usage}<span class="lpd-pill small {sv.usage.state === 'reported' ? 'active' : sv.usage.state === 'failed' ? 'error' : 'pending'}" title={sv.usage.detail ?? ''}>{t('usage.' + sv.usage.state)}</span>{/if}
                                {/if}
                                <button type="button" class="lpd-cell-button" onclick={() => removeSender(i)}>{t('Remove')}</button>
                            </li>
                        {/each}
                    </ul>
                {/if}
                <div class="lpd-add">
                    <MultiSelect options={senderOptions} bind:selected={picked} multiple={false} placeholder={t('editor.senders.pick')} inline={true} testId="sender" />
                    {#if mode === 'two-key'}
                        <select bind:value={pickedRole} aria-label={t('editor.senders.role')}>
                            <option value="up">{t('role.up.long')}</option>
                            <option value="down">{t('role.down.long')}</option>
                        </select>
                    {/if}
                    <button type="button" onclick={addPicked} disabled={!picked[0]}>{t('Add')}</button>
                    <button type="button" onclick={listen} disabled={listening > 0}>{listening > 0 ? t('editor.senders.listening', {seconds: listening}) : t('editor.senders.listen')}</button>
                </div>
                {#if heard}
                    <div class="lpd-heard">
                        {t('editor.senders.heard', {name: `${labelOf(heard)} (${heard.interface})`})}
                        {#if heard.interface === targetInterface}
                            <div class="lpd-warn">{t('editor.senders.sameInterface')}</div>
                        {:else}
                            <button type="button" class="lpd-cell-button" onclick={() => heard && (addSender(heard), (heard = undefined))}>{t('Add')}</button>
                        {/if}
                    </div>
                {/if}
                {#if picked[0] && senders.find((s) => `${s.interface}|${s.address}` === picked[0])?.interface === targetInterface}
                    <div class="lpd-warn">{t('editor.senders.sameInterface')}</div>
                {/if}
            </div>

            <span class="lpd-label">{t('editor.keys')}</span>
            <div>
                {#if binding}
                    <span class="lpd-mono">{keyLabel(binding.up, 'up')} {binding.up_name ? `(${binding.up_name})` : ''} · {keyLabel(binding.down, 'down')} {binding.down_name ? `(${binding.down_name})` : ''}</span>
                {:else}
                    <div class="lpd-muted">{t('editor.keys.hint')} {targetInterface ? t('editor.keys.free', {count: freeKeys.length}) : ''}</div>
                    <label><input type="radio" name="keys" value="auto" checked={keysAuto} onchange={() => ((keysAuto = true), (plan = undefined))} /> {t('editor.keys.auto')}</label>
                    <label><input type="radio" name="keys" value="pick" checked={!keysAuto} onchange={() => ((keysAuto = false), (plan = undefined))} /> ▲ / ▼</label>
                    {#if !keysAuto}
                        <div class="lpd-add">
                            <MultiSelect options={keyOptions} bind:selected={up} multiple={false} placeholder="▲" inline={true} onchange={() => (plan = undefined)} testId="key-up" />
                            <MultiSelect options={keyOptions.filter((o) => o.value !== up[0])} bind:selected={down} multiple={false} placeholder="▼" inline={true} onchange={() => (plan = undefined)} testId="key-down" />
                        </div>
                    {/if}
                {/if}
            </div>

            <span class="lpd-label"><button type="button" class="lpd-link" onclick={() => (advanced = !advanced)}>{advanced ? '▾' : '▸'} {t('editor.advanced')}</button></span>
            <div></div>
            {#if advanced}
                {#if kind === 'dimmer'}
                    <label class="lpd-label" for="lpd-onlevel">{t('editor.onLevel')}</label>
                    <div>
                        <select id="lpd-onlevel" bind:value={onLevel} disabled={wired} onchange={() => (plan = undefined)}>
                            <option value="full">{t('editor.onLevel.full')}</option>
                            <option value="last">{t('editor.onLevel.last')}</option>
                        </select>
                        {#if wired}<span class="lpd-muted"> {t('editor.onLevel.wired')}</span>{/if}
                    </div>
                    <label class="lpd-label" for="lpd-step">{t('editor.step')}</label>
                    <div><input id="lpd-step" type="number" min="1" max="100" step="1" bind:value={stepPct} oninput={() => (plan = undefined)} /> <span class="lpd-muted">{t('editor.step.hint')}</span></div>
                    <label class="lpd-label" for="lpd-min">{t('editor.range')}</label>
                    <div><input id="lpd-min" type="number" min="0" max="99" bind:value={minPct} oninput={() => (plan = undefined)} /> – <input type="number" min="1" max="100" bind:value={maxPct} oninput={() => (plan = undefined)} aria-label={t('editor.range')} /> <span class="lpd-muted">{t('editor.range.hint')}</span></div>
                {:else}
                    <label class="lpd-label" for="lpd-travel">{t('editor.travel')}</label>
                    <div><input id="lpd-travel" type="number" min="0.1" max="25.5" step="0.1" bind:value={travel} oninput={() => (plan = undefined)} /> <span class="lpd-muted">{t('editor.travel.hint')}</span></div>
                {/if}
                <label class="lpd-label" for="lpd-interval">{t('editor.interval')}</label>
                <div><input id="lpd-interval" type="number" min="100" max="2000" step="10" bind:value={interval} /> <span class="lpd-muted">{t('editor.interval.hint', {ms: settings?.step_interval_ms ?? 300})}</span></div>
            {/if}
        </div>

        {#if result}
            <h3>{t('editor.applied')}</h3>
            <PlanView {result} />
        {:else if plan}
            <h3>{t('plan.title')}</h3>
            <PlanView {plan} />
        {/if}
        {#if error}<p class="lpd-danger">{t('error.generic', {message: error})}</p>{/if}

        {#if binding}
            {#if hmipSenders.length}
                <section class="lpd-card">
                    <h2>{t('editor.usage.title')}</h2>
                    <p class="lpd-muted">{t('usage.hint')}</p>
                    <ul>
                        {#each hmipSenders as s (s.address)}
                            <li>{s.name || s.address}: {s.usage ? t('usage.' + s.usage.state) : '–'} {#if s.usage?.detail}<span class="lpd-muted">({s.usage.detail})</span>{/if}</li>
                        {/each}
                    </ul>
                </section>
            {/if}
            <section class="lpd-card">
                <h2>{t('editor.test')}</h2>
                <p class="lpd-muted">{t('editor.test.hint')}</p>
                <button type="button" onclick={() => test('up', 'PRESS_SHORT')}>{t('editor.test.shortUp')}</button>
                <button type="button" onclick={() => test('up', 'PRESS_LONG')}>{t('editor.test.longUp')}</button>
                <button type="button" onclick={() => test('down', 'PRESS_SHORT')}>{t('editor.test.shortDown')}</button>
                <button type="button" onclick={() => test('down', 'PRESS_LONG')}>{t('editor.test.longDown')}</button>
            </section>
            <ExternalPanel iface={binding.target.interface} up={binding.up} down={binding.down} upName={binding.up_name} downName={binding.down_name} mode={binding.mode} kind={binding.kind} {notices} />
        {/if}
    {/if}
    {#snippet buttons()}
        <div class="lpd-dialog-buttons">
            <button type="button" onclick={onclose} disabled={busy}>{result ? t('Close') : t('Cancel')}</button>
            {#if !result}
                {#if binding && !optionsChanged}
                    <button type="button" class="primary" onclick={doSave} disabled={busy}>{t('Save')}</button>
                {:else}
                    <button type="button" onclick={doPlan} disabled={busy || (!binding && !target[0])} title={t('editor.plan.hint')}>{t('editor.plan')}</button>
                    <button type="button" class="primary" onclick={doApply} disabled={busy || !plan}>{t('editor.apply')}</button>
                {/if}
            {/if}
        </div>
    {/snippet}
</Dialog>

<style>
    .lpd-senders {
        list-style: none;
        margin: 4px 0;
        padding: 0;
    }
    .lpd-senders li {
        display: flex;
        gap: 8px;
        align-items: center;
        margin-bottom: 4px;
    }
    .lpd-senders li .lpd-truncate {
        flex: 1;
        min-width: 0;
    }
    .lpd-add {
        display: flex;
        gap: 8px;
        align-items: center;
        flex-wrap: wrap;
        margin-top: 4px;
    }
    .lpd-heard {
        margin-top: 6px;
    }
    h3 {
        font-size: 0.95rem;
        margin: 12px 0 4px;
    }
</style>

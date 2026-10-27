<script lang="ts">
    // "Remove this binding?" with the dry run of the removal - the kit's dialog, never
    // window.confirm. "Forget only" leaves the direct links on the actuator in place.
    import {Dialog} from 'occulite-ui';
    import {t} from 'occulite-ui/i18n';

    import PlanView from './PlanView.svelte';
    import {del, get} from './api.js';
    import type {Plan, Result, View} from './model.js';

    let {binding, onclose, ondone}: {binding: View | undefined; onclose: () => void; ondone: (r: Result) => void} = $props();
    const open = $derived(binding !== undefined);
    let forget = $state(false);
    let plan = $state<Plan | undefined>();
    let busy = $state(false);
    let error = $state('');
    let result = $state<Result | undefined>();

    $effect(() => {
        const b = binding;
        const f = forget;
        plan = undefined;
        result = undefined;
        error = '';
        if (!b) return;
        get<Plan>(`api/v1/bindings/${encodeURIComponent(b.id)}/remove-plan?forget=${f ? 1 : 0}`)
            .then((p) => (plan = p))
            .catch((e) => (error = (e as Error).message));
    });

    $effect(() => {
        if (binding?.adopted) forget = true;
    });

    async function confirm() {
        if (!binding) return;
        busy = true;
        error = '';
        try {
            result = await del<Result>(`api/v1/bindings/${encodeURIComponent(binding.id)}?forget=${forget ? 1 : 0}`);
            ondone(result);
        } catch (e) {
            const body = (e as {body?: {result?: Result}}).body;
            if (body?.result) result = body.result;
            error = (e as Error).message;
        } finally {
            busy = false;
        }
    }
</script>

<Dialog {open} title={t('binding.remove.title')} width="34rem" closable={!busy} {onclose} testId="binding-remove">
    {#if binding}
        <p>{t('binding.remove.question', {name: binding.target_name || binding.target.address})}</p>
        <label><input type="checkbox" bind:checked={forget} disabled={busy || !!result} /> {t('binding.remove.forget')}</label>
        {#if result}
            <PlanView {result} />
        {:else if plan}
            <PlanView {plan} />
        {:else if !error}
            <p class="lpd-empty">{t('Loading…')}</p>
        {/if}
        {#if error}<p class="lpd-danger">{t('error.generic', {message: error})}</p>{/if}
    {/if}
    {#snippet buttons()}
        <div class="lpd-dialog-buttons">
            <button type="button" onclick={onclose} disabled={busy}>{result ? t('Close') : t('Cancel')}</button>
            {#if !result}
                <button type="button" class="primary danger" disabled={busy || !plan} onclick={confirm}>{t('Remove')}</button>
            {/if}
        </div>
    {/snippet}
</Dialog>

<script lang="ts">
    // The dry run and its outcome, step by step: what the addon writes to the system before the
    // user confirms, and what happened to each step afterwards.
    import {t} from 'occulite-ui/i18n';

    import type {Plan, Result, Step, StepResult} from './model.js';

    let {plan, result}: {plan?: Plan | undefined; result?: Result | undefined} = $props();

    const steps = $derived<{step: Step; state?: string; detail?: string}[]>(
        result ? result.steps.map((r: StepResult) => ({step: r.step, state: r.state, detail: r.detail})) : (plan?.steps ?? []).map((s) => ({step: s})),
    );
    const warnings = $derived(result?.plan.warnings ?? plan?.warnings ?? []);

    function params(p: Record<string, unknown> | undefined): string {
        if (!p) return '';
        return Object.keys(p)
            .sort()
            .map((k) => `${k} = ${String(p[k])}`)
            .join(', ');
    }

    function metaWhat(s: Step): string {
        const parts: string[] = [];
        if (s.name !== undefined && s.name !== null) parts.push(s.name === '' ? t('plan.setMeta.unname') : t('plan.setMeta.name', {name: s.name}));
        parts.push(s.marker ? t('plan.setMeta.mark') : t('plan.setMeta.unmark'));
        return parts.join(', ');
    }

    function nothing(s: Step): boolean {
        return s.unchanged === true || (s.kind === 'putParamset' && Object.keys(s.params ?? {}).length === 0);
    }
</script>

{#if steps.length === 0}
    <p class="lpd-empty">{t('plan.nothing')}</p>
{:else}
    <ol class="lpd-steps">
        {#each steps as {step, state, detail} (step.kind + step.key + step.receiver)}
            <li class:lpd-muted={nothing(step) && !state}>
                {#if step.kind === 'addLink'}
                    {t('plan.addLink', {key: step.key, receiver: step.receiver ?? '', role: t('role.' + step.role)})}
                {:else if step.kind === 'putParamset'}
                    {#if Object.keys(step.params ?? {}).length === 0}
                        {t('plan.putParamset.none', {key: step.key, receiver: step.receiver ?? ''})}
                    {:else}
                        {t('plan.putParamset', {key: step.key, receiver: step.receiver ?? ''})}
                        <div class="lpd-params lpd-mono">{params(step.params)}</div>
                    {/if}
                {:else if step.kind === 'removeLink'}
                    {t('plan.removeLink', {key: step.key, receiver: step.receiver ?? ''})}
                {:else}
                    {t('plan.setMeta', {key: step.key, what: metaWhat(step)})}
                {/if}
                {#if step.missing?.length}
                    <div class="lpd-params lpd-warn">{t('plan.missing', {params: step.missing.join(', ')})}</div>
                {/if}
                {#if state}
                    <span class="lpd-pill small {state === 'failed' ? 'error' : state === 'pending' ? 'pending' : state === 'done' ? 'active' : ''}" title={detail ?? ''}>{t('step.' + state)}</span>
                    {#if detail}<span class="lpd-muted"> {detail}</span>{/if}
                {/if}
            </li>
        {/each}
    </ol>
{/if}
{#if warnings.length}
    <p class="lpd-warn"><strong>{t('plan.warnings')}:</strong> {warnings.join(' · ')}</p>
{/if}
{#if result?.failed}
    <p class="lpd-danger">{t('error.generic', {message: result.failed})}</p>
{/if}

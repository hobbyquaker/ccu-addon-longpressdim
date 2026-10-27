<script lang="ts">
    // The import of a Node-RED mapping: the pasted `'<sender name>': '<key name>'` lines are
    // resolved through the system's names, shown with what each would do, and the resolved
    // senders added to their bindings.
    import {t} from 'occulite-ui/i18n';
    import type {NoticesStore} from 'occulite-ui/notices';

    import {post} from './api.js';
    import type {MappingEntry} from './model.js';

    let {notices, onchanged}: {notices: NoticesStore; onchanged?: () => void} = $props();

    let text = $state('');
    let entries = $state<MappingEntry[] | undefined>();
    let busy = $state(false);

    async function parse() {
        busy = true;
        try {
            const r = await post<{entries: MappingEntry[]}>('api/v1/import/parse', {text});
            entries = r.entries;
        } catch (e) {
            notices.push('error', (e as Error).message);
        } finally {
            busy = false;
        }
    }

    async function apply() {
        if (!entries) return;
        busy = true;
        try {
            const r = await post<{added: number; skipped: number; error?: string}>('api/v1/import/apply', {entries});
            notices.push(r.error ? 'warn' : 'info', t('import.done', {added: r.added, skipped: r.skipped}) + (r.error ? ` ${r.error}` : ''));
            onchanged?.();
            await parse();
        } catch (e) {
            notices.push('error', (e as Error).message);
        } finally {
            busy = false;
        }
    }

    const ready = $derived(entries?.filter((e) => !e.error && e.binding).length ?? 0);
</script>

<p class="lpd-muted">{t('import.intro')}</p>
<textarea class="lpd-paste" rows="8" bind:value={text} spellcheck="false" placeholder={"'Taster Flur:1': 'Licht Flur DimUp',\n'Taster Flur:2': 'Licht Flur DimDown'"}></textarea>
<div class="lpd-toolbar">
    <button type="button" class="primary" onclick={parse} disabled={busy || !text.trim()}>{t('import.parse')}</button>
    <button type="button" onclick={apply} disabled={busy || ready === 0}>{t('import.apply')} ({ready})</button>
</div>
{#if entries}
    {#if entries.length === 0}
        <p class="lpd-empty">{t('import.none')}</p>
    {:else}
        <table class="lpd-plain">
            <thead><tr><th>{t('import.sender')}</th><th>{t('import.key')}</th><th>{t('import.binding')}</th><th>{t('Status')}</th></tr></thead>
            <tbody>
                {#each entries as e, i (i)}
                    <tr>
                        <td>{e.sender_name}{#if e.sender}<span class="lpd-muted lpd-mono"> → {e.sender.interface} {e.sender.address}</span>{/if}</td>
                        <td>{e.key_name}{#if e.key}<span class="lpd-muted lpd-mono"> → {e.key}</span>{/if}</td>
                        <td>{e.binding ?? '–'} {e.role ? `(${t('role.' + e.role)})` : ''}</td>
                        <td>{#if e.error}<span class="lpd-warn">{e.error}</span>{:else}<span class="lpd-pill small active">{t('import.ok')}</span>{/if}</td>
                    </tr>
                {/each}
            </tbody>
        </table>
        <p class="lpd-muted">{t('import.afterwards')}</p>
    {/if}
{/if}

<style>
    .lpd-paste {
        width: 100%;
        max-width: 52rem;
        box-sizing: border-box;
        font-family: var(--hmm-font-mono, ui-monospace, monospace);
        font-size: 0.85em;
        margin-bottom: 8px;
    }
    .lpd-plain {
        border-collapse: collapse;
        width: 100%;
        max-width: 60rem;
    }
    .lpd-plain th,
    .lpd-plain td {
        text-align: left;
        padding: 2px 8px 2px 0;
        border-bottom: 1px solid var(--hmm-border, #8884);
        vertical-align: top;
    }
    .lpd-plain th {
        font-weight: 600;
        opacity: 0.8;
    }
</style>

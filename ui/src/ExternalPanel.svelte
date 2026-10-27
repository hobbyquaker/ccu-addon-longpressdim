<script lang="ts">
    // "Press it from outside": the binding's two virtual keys, what each does, the rule for a
    // hold, and copyable examples for lite-rpc, XML-RPC, hm2mqtt.js and Node-RED.
    import {t} from 'occulite-ui/i18n';
    import {copyText} from 'occulite-ui/clipboard';
    import type {NoticesStore} from 'occulite-ui/notices';

    import type {Kind, Mode} from './model.js';

    let {iface, up, down, upName, downName, mode, kind, notices}: {iface: string; up: string; down: string; upName?: string; downName?: string; mode: Mode; kind: Kind; notices?: NoticesStore} = $props();

    const host = $derived(location.host || '<system>');
    const port = $derived(iface === 'BidCos-Wired' ? 2000 : iface === 'HmIP-RF' ? 2010 : 2001);
    const longUp = $derived(kind === 'blind' ? t('external.long.open') : t('external.long.up'));
    const longDown = $derived(kind === 'blind' ? t('external.long.close') : t('external.long.down'));
    const shortUp = $derived(mode === 'one-key' ? t('external.short.toggle') : t('external.short.on'));
    const shortDown = $derived(mode === 'one-key' ? t('external.short.none') : t('external.short.off'));

    function lite(key: string): string {
        return `curl -u :<token> -X POST https://${host}/api/rpc/v1/json/${iface} \\\n  -d '{"jsonrpc":"2.0","method":"setValue","params":["${key}","PRESS_LONG",true],"id":1}'`;
    }
    function xmlrpc(key: string): string {
        return `setValue("${key}", "PRESS_LONG", true)  # http://${location.hostname || '<system>'}:${port}`;
    }
    function mqtt(key: string, name: string | undefined): string {
        return `<prefix>/set/${name || key}/PRESS_LONG  true`;
    }

    async function copy(text: string) {
        const out = await copyText(text);
        notices?.push(out === 'failed' ? 'error' : 'info', out === 'failed' ? t('error.generic', {message: 'clipboard'}) : t('Copied'));
    }
</script>

<section class="lpd-card">
    <h2>{t('external.title')}</h2>
    <p class="lpd-muted">{t('external.intro')}</p>
    <dl class="lpd-kv">
        <dt>▲</dt>
        <dd><span class="lpd-mono">{up}</span> {upName ? `(${upName})` : ''} - {t('external.up', {short: shortUp, long: longUp})}</dd>
        <dt>▼</dt>
        <dd><span class="lpd-mono">{down}</span> {downName ? `(${downName})` : ''} - {t('external.down', {short: shortDown, long: longDown})}</dd>
    </dl>
    <h3 class="lpd-h3">{t('external.lite')}</h3>
    <pre class="lpd-code">{lite(up)}</pre>
    <button type="button" class="lpd-cell-button" onclick={() => copy(lite(up))}>{t('Copy')} ▲</button>
    <button type="button" class="lpd-cell-button" onclick={() => copy(lite(down))}>{t('Copy')} ▼</button>
    <h3 class="lpd-h3">{t('external.xmlrpc')}</h3>
    <pre class="lpd-code">{xmlrpc(up)}
{xmlrpc(down)}</pre>
    <h3 class="lpd-h3">{t('external.mqtt')}</h3>
    <pre class="lpd-code">{mqtt(up, upName)}
{mqtt(down, downName)}</pre>
    <h3 class="lpd-h3">{t('external.nodered')}</h3>
    <p class="lpd-muted">{t('external.nodered.text', {iface, key: up})}</p>
</section>

<style>
    .lpd-h3 {
        font-size: 0.9rem;
        margin: 10px 0 2px;
    }
</style>

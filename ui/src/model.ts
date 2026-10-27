// The API's shapes as the app reads them (docs/openapi.json is the reference).

export type Kind = 'dimmer' | 'blind';
export type Mode = 'two-key' | 'one-key';
export type SenderRole = 'up' | 'down' | 'both';
export type State = 'active' | 'pending' | 'changed' | 'missing-link' | 'error' | 'unknown';

export interface Usage {
    state: 'reported' | 'pending' | 'failed' | 'released';
    detail?: string;
    at?: string;
}

export interface Sender {
    interface: string;
    address: string;
    role: SenderRole;
    usage?: Usage;
}

export interface SenderView extends Sender {
    name?: string;
    device?: string;
    rooms?: string[];
}

export interface Options {
    on_level?: number;
    step?: number;
    travel?: number;
    min_level?: number;
    max_level?: number;
}

export interface Binding {
    id: string;
    target: {interface: string; address: string; type: string; family: string};
    mode: Mode;
    up: string;
    down: string;
    senders: Sender[];
    options: Options;
    step_interval_ms?: number;
    adopted?: boolean;
    created: string;
    updated: string;
}

export interface Status {
    state: State;
    detail?: string;
    differs?: Record<string, Record<string, unknown>>;
    missing?: string[];
    checked_at?: string;
}

export interface Activity {
    last_hold?: string;
    last_direction?: string;
    steps: number;
    dropped: number;
    last_short?: string;
    holding: boolean;
    level: number;
    errors: number;
}

export interface View extends Binding {
    target_name?: string;
    target_device?: string;
    target_rooms?: string[];
    kind: Kind;
    up_name?: string;
    down_name?: string;
    sender_views: SenderView[];
    status: Status;
    activity: Activity;
}

export interface Channel {
    interface: string;
    address: string;
    type: string;
    device: string;
    family?: string;
    kind?: Kind;
    name?: string;
    rooms?: string[];
}

export interface Key {
    n: number;
    address: string;
    name?: string;
    links?: {SENDER: string; RECEIVER: string; NAME: string; DESCRIPTION: string}[];
    marker?: {binding: string; role: string};
    foreign?: boolean;
    free: boolean;
}

export interface Step {
    kind: 'addLink' | 'putParamset' | 'removeLink' | 'setMeta';
    interface: string;
    key: string;
    receiver?: string;
    role?: string;
    params?: Record<string, unknown>;
    missing?: string[];
    name?: string | null;
    marker?: {binding: string; role: string} | null;
    note?: string;
    unchanged?: boolean;
}

export interface Plan {
    binding: Binding;
    steps: Step[];
    warnings?: string[];
}

export interface StepResult {
    step: Step;
    state: 'done' | 'pending' | 'skipped' | 'failed';
    detail?: string;
}

export interface Result {
    plan: Plan;
    steps: StepResult[];
    pending: boolean;
    differs?: Record<string, Record<string, unknown>>;
    failed?: string;
}

export interface Classification {
    long: string;
    short: string;
    role?: string;
    step?: number;
    on_level?: number;
}

export interface ForeignLink {
    interface: string;
    key: string;
    key_name?: string;
    Receiver: Channel;
    link: {SENDER: string; RECEIVER: string; NAME: string; DESCRIPTION: string};
    profile?: Classification;
    error?: string;
}

export interface MappingEntry {
    sender_name: string;
    key_name: string;
    sender?: Sender;
    key_interface?: string;
    key?: string;
    binding?: string;
    role?: string;
    error?: string;
}

export interface Settings {
    step_interval_ms: number;
    hold_timeout_ms: number;
    log_presses: boolean;
}

export interface Health {
    ok: boolean;
    version: string;
    platform: string;
    uptime_s: number;
    service: {stream: boolean; stream_detail?: string; bindings: number; presses: number; pending: number; changed: number};
}

export function formatDateTime(iso: string | undefined, lang: string): string {
    if (!iso) return '';
    const d = new Date(iso);
    if (isNaN(d.getTime()) || d.getFullYear() < 2000) return '';
    return d.toLocaleString(lang === 'de' ? 'de-DE' : 'en-GB', {dateStyle: 'short', timeStyle: 'short'});
}

export function formatTime(iso: string | undefined, lang: string): string {
    if (!iso) return '';
    const d = new Date(iso);
    if (isNaN(d.getTime()) || d.getFullYear() < 2000) return '';
    return d.toLocaleTimeString(lang === 'de' ? 'de-DE' : 'en-GB', {timeStyle: 'medium'});
}

/** The key's number of a virtual key address ("HmIP-RCV-1:48" -> 48). */
export function keyNumber(address: string): number {
    const i = address.lastIndexOf(':');
    return i < 0 ? 0 : Number(address.slice(i + 1));
}

/** The short label of a key: "▲ 48" / "▼ 47". */
export function keyLabel(address: string, direction: 'up' | 'down'): string {
    return `${direction === 'up' ? '▲' : '▼'} ${keyNumber(address)}`;
}

/** A percentage of a level (0.05 -> "5 %"). */
export function percent(v: number | undefined, fallback = 5): string {
    const n = v && v > 0 ? v : fallback / 100;
    return `${Math.round(n * 1000) / 10} %`;
}

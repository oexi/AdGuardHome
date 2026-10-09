import { customFetch } from './customFetch';

export type UpstreamSubscription = {
    id: number;
    name: string;
    url: string;
    enabled: boolean;
    upstreams: string[];
    rules_count: number;
    invalid_count: number;
    last_updated?: string;
    last_error?: string;
};

export type UpstreamSubscriptionsStatus = {
    enabled: boolean;
    interval: number;
    subscriptions: UpstreamSubscription[];
};

export type UpstreamSubscriptionData = {
    name: string;
    url: string;
    enabled?: boolean;
    upstreams: string[];
};

export type UpstreamSubscriptionsConfig = {
    enabled?: boolean;
    interval?: number;
};

export type UpstreamSubscriptionsRefreshResponse = {
    updated: number;
    errors: string[];
};

const BASE = 'control/upstream_subscriptions';

const post = <T = void>(path: string, body: unknown): Promise<T> =>
    customFetch<T>(`${BASE}/${path}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
    });

export const upstreamSubscriptionsStatus = (): Promise<UpstreamSubscriptionsStatus> =>
    customFetch<UpstreamSubscriptionsStatus>(`${BASE}/status`, { method: 'GET' });

export const upstreamSubscriptionsConfig = (config: UpstreamSubscriptionsConfig) =>
    post('config', config);

export const upstreamSubscriptionsAdd = (data: UpstreamSubscriptionData) => post('add', data);

export const upstreamSubscriptionsSet = (id: number, data: UpstreamSubscriptionData) =>
    post('set', { id, data });

export const upstreamSubscriptionsRemove = (id: number) => post('remove', { id });

export const upstreamSubscriptionsRefresh = (id?: number) =>
    post<UpstreamSubscriptionsRefreshResponse>('refresh', id ? { id } : {});

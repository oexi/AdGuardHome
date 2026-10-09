import apiClient from '../../../../api/Api';

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

export type UpstreamSubscriptionsRefreshResponse = {
    updated: number;
    errors: string[];
};

const PATH = 'upstream_subscriptions';

const post = (path: string, data: unknown) => apiClient.makeRequest(`${PATH}/${path}`, 'POST', { data });

export const getStatus = (): Promise<UpstreamSubscriptionsStatus> => apiClient.makeRequest(`${PATH}/status`, 'GET');

export const setConfig = (config: { enabled?: boolean; interval?: number }) => post('config', config);

export const addSubscription = (data: UpstreamSubscriptionData) => post('add', data);

export const setSubscription = (id: number, data: UpstreamSubscriptionData) => post('set', { id, data });

export const removeSubscription = (id: number) => post('remove', { id });

export const refreshSubscriptions = (id?: number): Promise<UpstreamSubscriptionsRefreshResponse> =>
    post('refresh', id ? { id } : {});

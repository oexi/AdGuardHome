import { createStore } from 'solid-js/store';
import { untrack } from 'solid-js';

import {
    upstreamSubscriptionsAdd,
    upstreamSubscriptionsConfig,
    upstreamSubscriptionsRefresh,
    upstreamSubscriptionsRemove,
    upstreamSubscriptionsSet,
    upstreamSubscriptionsStatus,
    type UpstreamSubscription,
    type UpstreamSubscriptionData,
    type UpstreamSubscriptionsConfig,
} from 'panel/api/upstreamSubscriptions';
import intl from 'panel/common/intl';
import { addErrorToast, addSuccessToast } from './toasts';

type UpstreamSubscriptionsState = {
    loaded: boolean;
    processing: boolean;
    processingUpdate: boolean;
    processingRefresh: boolean;
    enabled: boolean;
    interval: number;
    subscriptions: UpstreamSubscription[];
};

const [state, setState] = createStore<UpstreamSubscriptionsState>({
    loaded: false,
    processing: false,
    processingUpdate: false,
    processingRefresh: false,
    enabled: true,
    interval: 24,
    subscriptions: [],
});

export const upstreamSubscriptionsState = untrack(() => state);

export const getUpstreamSubscriptions = async () => {
    setState('processing', true);
    try {
        const data = await upstreamSubscriptionsStatus();
        setState({
            enabled: data.enabled,
            interval: data.interval,
            subscriptions: data.subscriptions || [],
            loaded: true,
        });
    } catch (error) {
        addErrorToast({ error });
    } finally {
        setState('processing', false);
    }
};

/**
 * Wraps a modifying request: sets the processing flag, refreshes the list
 * afterwards, and reports errors.  Returns true on success.
 */
const modify = async (request: () => Promise<unknown>, successMessage?: string) => {
    setState('processingUpdate', true);
    try {
        await request();
        if (successMessage) {
            addSuccessToast(successMessage);
        }
        await getUpstreamSubscriptions();
        return true;
    } catch (error) {
        addErrorToast({ error });
        return false;
    } finally {
        setState('processingUpdate', false);
    }
};

export const setUpstreamSubscriptionsConfig = (config: UpstreamSubscriptionsConfig) =>
    modify(() => upstreamSubscriptionsConfig(config));

export const toggleUpstreamSubscriptions = () =>
    setUpstreamSubscriptionsConfig({ enabled: !state.enabled });

export const addUpstreamSubscription = (data: UpstreamSubscriptionData) =>
    modify(
        () => upstreamSubscriptionsAdd(data),
        intl.getMessage('upstream_subscription_added', { value: data.name || data.url }),
    );

export const editUpstreamSubscription = (id: number, data: UpstreamSubscriptionData) =>
    modify(() => upstreamSubscriptionsSet(id, data));

export const toggleUpstreamSubscription = (sub: UpstreamSubscription) =>
    editUpstreamSubscription(sub.id, {
        name: sub.name,
        url: sub.url,
        upstreams: sub.upstreams,
        enabled: !sub.enabled,
    });

export const removeUpstreamSubscription = (sub: UpstreamSubscription) =>
    modify(
        () => upstreamSubscriptionsRemove(sub.id),
        intl.getMessage('upstream_subscription_removed', { value: sub.name || sub.url }),
    );

export const refreshUpstreamSubscriptions = async (id?: number) => {
    setState('processingRefresh', true);
    try {
        const resp = await upstreamSubscriptionsRefresh(id);
        if (resp.errors?.length) {
            addErrorToast({ error: resp.errors.join('\n') });
        }

        if (resp.updated > 0) {
            addSuccessToast(intl.getPlural('list_updated', resp.updated));
        } else if (!resp.errors?.length) {
            addSuccessToast(intl.getMessage('all_lists_up_to_date_toast'));
        }

        await getUpstreamSubscriptions();
    } catch (error) {
        addErrorToast({ error });
    } finally {
        setState('processingRefresh', false);
    }
};

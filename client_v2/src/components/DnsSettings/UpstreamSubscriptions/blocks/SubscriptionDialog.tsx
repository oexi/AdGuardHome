import { createEffect, createSignal, type Accessor } from 'solid-js';

import intl from 'panel/common/intl';
import { ConfigDialog } from 'panel/common/ui/ConfigDialog';
import { Input } from 'panel/common/controls/Input';
import { Textarea } from 'panel/common/controls/Textarea';
import { validateRequiredValue } from 'panel/helpers/validators';
import theme from 'panel/lib/theme';
import type { UpstreamSubscription } from 'panel/api/upstreamSubscriptions';
import {
    addUpstreamSubscription,
    editUpstreamSubscription,
    upstreamSubscriptionsState as state,
} from 'panel/stores/upstreamSubscriptions';

const R_HTTP_URL = /^https?:\/\/[^\s/]+/i;

type Props = {
    open: Accessor<boolean>;
    onClose: () => void;
    /** The subscription to edit; a new one is added when undefined. */
    subscription?: UpstreamSubscription;
};

const splitUpstreams = (text: string) =>
    text
        .split('\n')
        .map((line) => line.trim())
        .filter((line) => line && !line.startsWith('#'));

export const SubscriptionDialog = (props: Props) => {
    const [name, setName] = createSignal('');
    const [url, setUrl] = createSignal('');
    const [upstreams, setUpstreams] = createSignal('');
    const [urlError, setUrlError] = createSignal('');

    createEffect(() => {
        if (props.open()) {
            const sub = props.subscription;
            setName(sub?.name ?? '');
            setUrl(sub?.url ?? '');
            setUpstreams(sub?.upstreams?.join('\n') ?? '');
            setUrlError('');
        }
    });

    const validateUrl = () => {
        const value = url().trim();
        const err =
            validateRequiredValue(value) ||
            (R_HTTP_URL.test(value) ? '' : intl.getMessage('upstream_subscription_url_error'));
        setUrlError(err || '');

        return !err;
    };

    const onSubmit = async () => {
        if (!validateUrl()) {
            return;
        }

        const data = {
            name: name().trim(),
            url: url().trim(),
            upstreams: splitUpstreams(upstreams()),
        };

        const ok = props.subscription
            ? await editUpstreamSubscription(props.subscription.id, {
                  ...data,
                  enabled: props.subscription.enabled,
              })
            : await addUpstreamSubscription({ ...data, enabled: true });
        if (ok) {
            props.onClose();
        }
    };

    return (
        <ConfigDialog
            open={props.open()}
            title={intl.getMessage(
                props.subscription ? 'upstream_subscription_edit' : 'upstream_subscription_add',
            )}
            description={intl.getMessage('upstream_subscription_dialog_desc')}
            onClose={props.onClose}
            onSubmit={onSubmit}
            processing={state.processingUpdate}
            buttonText={intl.getMessage(props.subscription ? 'save' : 'add')}
        >
            <div class={theme.form.group}>
                <div class={theme.form.input}>
                    <Input
                        type="text"
                        id="upstream_subscription_name"
                        name="name"
                        label={intl.getMessage('name_label')}
                        placeholder={intl.getMessage('upstream_subscription_name_placeholder')}
                        value={name()}
                        onChange={(e) => setName(e.target.value)}
                    />
                </div>

                <div class={theme.form.input}>
                    <Input
                        type="text"
                        id="upstream_subscription_url"
                        name="url"
                        label={intl.getMessage('url_label')}
                        placeholder="https://example.org/upstreams.txt"
                        value={url()}
                        onChange={(e) => setUrl(e.target.value)}
                        onBlur={validateUrl}
                        errorMessage={urlError()}
                    />
                </div>

                <div class={theme.form.input}>
                    <Textarea
                        id="upstream_subscription_upstreams"
                        label={intl.getMessage('upstream_subscription_upstreams_label')}
                        placeholder={intl.getMessage('upstream_subscription_upstreams_placeholder')}
                        value={upstreams()}
                        onChange={(e) => setUpstreams(e.target.value)}
                        size="medium"
                        highlightComments
                    />
                </div>
            </div>
        </ConfigDialog>
    );
};

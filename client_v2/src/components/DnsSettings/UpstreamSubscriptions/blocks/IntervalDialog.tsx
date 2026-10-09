import { createEffect, createSignal, type Accessor } from 'solid-js';

import intl from 'panel/common/intl';
import { ConfigDialog } from 'panel/common/ui/ConfigDialog';
import { Radio } from 'panel/common/controls/Radio';
import theme from 'panel/lib/theme';
import {
    setUpstreamSubscriptionsConfig,
    upstreamSubscriptionsState as state,
} from 'panel/stores/upstreamSubscriptions';

const INTERVALS = [0, 1, 6, 12, 24, 72, 168];

export const getIntervalTitle = (hours: number) => {
    switch (hours) {
        case 0:
            return intl.getMessage('update_filters_disable');
        case 1:
            return intl.getMessage('update_filters_interval_hourly');
        case 24:
            return intl.getMessage('update_filters_interval_daily');
        case 168:
            return intl.getMessage('update_filters_interval_weekly');
        default:
            return hours % 24 === 0
                ? intl.getPlural('settings_days', hours / 24)
                : intl.getPlural('settings_hours', hours);
    }
};

type Props = {
    open: Accessor<boolean>;
    onClose: () => void;
};

export const IntervalDialog = (props: Props) => {
    const [value, setValue] = createSignal(state.interval);

    createEffect(() => {
        if (props.open()) {
            setValue(state.interval);
        }
    });

    const options = () => {
        const values = INTERVALS.includes(state.interval)
            ? INTERVALS
            : [...INTERVALS, state.interval].sort((a, b) => a - b);

        return values.map((v) => ({ text: getIntervalTitle(v), value: v }));
    };

    const onSubmit = async () => {
        const ok = await setUpstreamSubscriptionsConfig({ interval: value() });
        if (ok) {
            props.onClose();
        }
    };

    return (
        <ConfigDialog
            open={props.open()}
            title={intl.getMessage('upstream_subscriptions_interval_title')}
            description={intl.getMessage('upstream_subscriptions_interval_desc')}
            onClose={props.onClose}
            onSubmit={onSubmit}
            processing={state.processingUpdate}
        >
            <div class={theme.form.group}>
                <Radio
                    name="upstream_subscriptions_interval"
                    value={value()}
                    options={options()}
                    handleChange={(v: number) => setValue(v)}
                    inModal
                />
            </div>
        </ConfigDialog>
    );
};

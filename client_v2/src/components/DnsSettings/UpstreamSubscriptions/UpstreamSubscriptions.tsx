import { createMemo, createSignal, onMount, Show } from 'solid-js';
import cn from 'clsx';

import intl from 'panel/common/intl';
import theme from 'panel/lib/theme';
import { Breadcrumbs } from 'panel/common/ui/Breadcrumbs';
import { SettingRow } from 'panel/common/ui/SettingRow';
import { Icon } from 'panel/common/ui/Icon';
import { PlusButton } from 'panel/common/ui/PlusButton';
import { PageLoader } from 'panel/common/ui/Loader';
import { ConfirmDialog } from 'panel/common/ui/ConfirmDialog';
import { Table, type TableColumn } from 'panel/common/ui/Table';
import { Switch } from 'panel/common/controls/Switch';
import { RoutePath } from 'panel/components/Routes/Paths';
import { formatShortDateTime } from 'panel/helpers/helpers';
import { useDialog } from 'panel/hooks/useDialog';
import type { UpstreamSubscription } from 'panel/api/upstreamSubscriptions';
import {
    getUpstreamSubscriptions,
    refreshUpstreamSubscriptions,
    removeUpstreamSubscription,
    toggleUpstreamSubscription,
    toggleUpstreamSubscriptions,
    upstreamSubscriptionsState as state,
} from 'panel/stores/upstreamSubscriptions';

import { SubscriptionDialog } from './blocks/SubscriptionDialog';
import { IntervalDialog, getIntervalTitle } from './blocks/IntervalDialog';
import s from './UpstreamSubscriptions.module.pcss';

export const UpstreamSubscriptions = () => {
    const subscriptionDialog = useDialog();
    const intervalDialog = useDialog();

    const [current, setCurrent] = createSignal<UpstreamSubscription | undefined>();
    const [toDelete, setToDelete] = createSignal<UpstreamSubscription | undefined>();

    onMount(() => {
        getUpstreamSubscriptions();
    });

    const busy = () => state.processingUpdate || state.processingRefresh;

    const openAdd = () => {
        setCurrent(undefined);
        subscriptionDialog.openDialog();
    };

    const openEdit = (sub: UpstreamSubscription) => {
        setCurrent(sub);
        subscriptionDialog.openDialog();
    };

    const columns = createMemo<TableColumn<UpstreamSubscription>[]>(() => [
        {
            key: 'name',
            header: { text: intl.getMessage('name_label'), class: s.headerCell },
            accessor: 'name',
            sortable: true,
            render: (value: string, row: UpstreamSubscription) => (
                <div class={theme.table.cell}>
                    <span class={theme.table.cellLabel}>{intl.getMessage('name_label')}</span>

                    <div class={cn(theme.table.cellValueText, s.nameCellValue)}>
                        <span class={theme.common.textOverflow}>{value}</span>
                        <Switch
                            id={`upstream_subscription_${row.id}`}
                            checked={row.enabled}
                            onChange={() => toggleUpstreamSubscription(row)}
                            disabled={busy()}
                        />
                    </div>
                </div>
            ),
        },
        {
            key: 'url',
            header: { text: intl.getMessage('url_label'), class: s.headerCell },
            accessor: 'url',
            sortable: true,
            render: (value: string) => (
                <div class={theme.table.cell}>
                    <span class={theme.table.cellLabel}>{intl.getMessage('url_label')}</span>

                    <div class={theme.table.cellValueText}>
                        <a
                            href={value}
                            class={cn(theme.link.link, theme.common.textOverflow)}
                            target="_blank"
                            rel="noopener noreferrer nofollow"
                            title={value}
                        >
                            {value}
                        </a>
                    </div>
                </div>
            ),
        },
        {
            key: 'upstreams',
            header: {
                text: intl.getMessage('upstream_subscription_upstreams'),
                class: s.headerCell,
            },
            accessor: 'upstreams',
            render: (value: string[]) => (
                <div class={theme.table.cell}>
                    <span class={theme.table.cellLabel}>
                        {intl.getMessage('upstream_subscription_upstreams')}
                    </span>

                    <div class={theme.table.cellValueText}>
                        <Show
                            when={value?.length}
                            fallback={
                                <span class={s.muted}>
                                    {intl.getMessage('upstream_subscription_upstreams_from_list')}
                                </span>
                            }
                        >
                            <span class={cn(s.code, theme.common.textOverflow)}>
                                {value.join(', ')}
                            </span>
                        </Show>
                    </div>
                </div>
            ),
        },
        {
            key: 'rules_count',
            header: { text: intl.getMessage('rules_label'), class: s.headerCell },
            accessor: 'rules_count',
            sortable: true,
            render: (value: number, row: UpstreamSubscription) => (
                <div class={theme.table.cell}>
                    <span class={theme.table.cellLabel}>{intl.getMessage('rules_label')}</span>

                    <div class={theme.table.cellValueText}>
                        <span
                            title={
                                row.invalid_count
                                    ? intl.getMessage('upstream_subscription_invalid_lines', {
                                          count: row.invalid_count,
                                      })
                                    : undefined
                            }
                        >
                            {value?.toLocaleString() || 0}
                        </span>
                    </div>
                </div>
            ),
        },
        {
            key: 'last_updated',
            header: { text: intl.getMessage('last_updated_label'), class: s.headerCell },
            accessor: 'last_updated',
            sortable: true,
            render: (value: string | undefined, row: UpstreamSubscription) => (
                <div class={theme.table.cell}>
                    <span class={theme.table.cellLabel}>
                        {intl.getMessage('last_updated_label')}
                    </span>

                    <div class={theme.table.cellValueText}>
                        <Show
                            when={!row.last_error}
                            fallback={
                                <span class={s.error} title={row.last_error}>
                                    {intl.getMessage('upstream_subscription_update_failed')}
                                </span>
                            }
                        >
                            <span>{value ? formatShortDateTime(value) : '—'}</span>
                        </Show>
                    </div>
                </div>
            ),
        },
        {
            key: 'actions',
            header: { text: '', class: s.headerCell },
            accessor: 'id',
            sortable: false,
            width: 120,
            render: (_: number, row: UpstreamSubscription) => (
                <div class={theme.table.cell}>
                    <div class={theme.table.cellValue}>
                        <div class={theme.table.cellActions}>
                            <button
                                type="button"
                                onClick={() => refreshUpstreamSubscriptions(row.id)}
                                disabled={busy() || !row.enabled}
                                class={theme.table.action}
                                title={intl.getMessage('check_updates_btn')}
                                aria-label={intl.getMessage('check_updates_btn')}
                                data-table-action
                            >
                                <Icon icon="refresh" color="gray" />
                                <span class={theme.table.actionLabel}>
                                    {intl.getMessage('check_updates_btn')}
                                </span>
                            </button>

                            <button
                                type="button"
                                onClick={() => openEdit(row)}
                                disabled={busy()}
                                class={theme.table.action}
                                title={intl.getMessage('edit_table_action')}
                                aria-label={intl.getMessage('edit_table_action')}
                                data-table-action
                            >
                                <Icon icon="edit" color="gray" />
                                <span class={theme.table.actionLabel}>
                                    {intl.getMessage('edit_table_action')}
                                </span>
                            </button>

                            <button
                                type="button"
                                onClick={() => setToDelete(row)}
                                disabled={busy()}
                                class={cn(theme.table.action, theme.table.action_danger)}
                                title={intl.getMessage('delete_table_action')}
                                aria-label={intl.getMessage('delete_table_action')}
                                data-table-action
                            >
                                <Icon icon="delete" color="red" />
                                <span class={theme.table.actionLabel}>
                                    {intl.getMessage('delete_table_action')}
                                </span>
                            </button>
                        </div>
                    </div>
                </div>
            ),
        },
    ]);

    const emptyTable = (
        <div class={s.emptyTableContent}>
            <Icon icon="not_found_search" color="gray" class={s.emptyTableIcon} />

            <div class={cn(theme.text.t3, s.emptyTableDesc)}>
                {intl.getMessage('upstream_subscriptions_empty')}
            </div>
        </div>
    );

    return (
        <div class={cn(theme.layout.container, s.container)}>
            <div class={theme.layout.containerIn}>
                <div class={s.breadcrumbs}>
                    <Breadcrumbs
                        parentLinks={[
                            { path: RoutePath.Dns, title: intl.getMessage('dns_settings') },
                        ]}
                        currentTitle={intl.getMessage('upstream_subscriptions')}
                    />
                </div>

                <Show when={state.loaded} fallback={<PageLoader />}>
                    <div class={s.header}>
                        <h1 class={cn(theme.layout.title, theme.title.h4, theme.title.h3_tablet)}>
                            {intl.getMessage('upstream_subscriptions')}
                        </h1>

                        <button
                            type="button"
                            onClick={() => refreshUpstreamSubscriptions()}
                            disabled={busy()}
                            class={cn(s.button, s.button_checkUpdates)}
                        >
                            <Icon icon="refresh" color="green" />
                            <span class={s.labelDesktop}>
                                {intl.getMessage('check_updates_btn')}
                            </span>
                        </button>

                        <button
                            type="button"
                            onClick={intervalDialog.openDialog}
                            disabled={busy()}
                            class={s.button}
                            title={getIntervalTitle(state.interval)}
                        >
                            <Icon icon="settings" color="green" />
                        </button>
                    </div>

                    <div class={s.desc}>
                        <p>{intl.getMessage('upstream_subscriptions_desc')}</p>
                        <p>{intl.getMessage('upstream_subscriptions_desc_formats')}</p>
                    </div>

                    <div class={s.switchRow}>
                        <SettingRow
                            variant="switch"
                            id="upstream_subscriptions_enabled"
                            title={intl.getMessage('upstream_subscriptions_enabled')}
                            description={intl.getMessage('upstream_subscriptions_enabled_desc')}
                            checked={state.enabled}
                            disabled={busy()}
                            onChange={() => toggleUpstreamSubscriptions()}
                        />
                    </div>

                    <div class={cn(s.group, s.buttonGroup)}>
                        <PlusButton onClick={openAdd} disabled={busy()}>
                            {intl.getMessage('upstream_subscription_add')}
                        </PlusButton>
                    </div>

                    <div class={cn(s.group, s.tableGroup)}>
                        <Table<UpstreamSubscription>
                            data={[...state.subscriptions]}
                            columns={columns()}
                            emptyTable={emptyTable}
                            getRowId={(row) => row.id}
                        />
                    </div>
                </Show>

                <SubscriptionDialog
                    open={subscriptionDialog.open}
                    onClose={subscriptionDialog.closeDialog}
                    subscription={current()}
                />

                <IntervalDialog open={intervalDialog.open} onClose={intervalDialog.closeDialog} />

                <Show when={toDelete()}>
                    {(sub) => (
                        <ConfirmDialog
                            onClose={() => setToDelete(undefined)}
                            onConfirm={() => {
                                removeUpstreamSubscription(sub()).then(() =>
                                    setToDelete(undefined),
                                );
                            }}
                            submitDisabled={busy()}
                            buttonText={intl.getMessage('remove')}
                            cancelText={intl.getMessage('cancel')}
                            title={intl.getMessage('upstream_subscription_remove')}
                            text={intl.getMessage('upstream_subscription_remove_desc', {
                                value: sub().name || sub().url,
                            })}
                            buttonVariant="danger"
                        />
                    )}
                </Show>
            </div>
        </div>
    );
};

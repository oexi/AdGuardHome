import React, { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useDispatch } from 'react-redux';
// @ts-expect-error FIXME: update react-table
import ReactTable from 'react-table';
import i18next from 'i18next';

import Card from '../../../ui/Card';
import CellWrap from '../../../ui/CellWrap';
import { Checkbox } from '../../../ui/Controls/Checkbox';
import { Select } from '../../../ui/Controls/Select';
import { addErrorToast, addSuccessToast } from '../../../../actions/toasts';
import { DAY_HOURS, FILTERS_INTERVALS_HOURS } from '../../../../helpers/constants';
import { formatDetailedDateTime } from '../../../../helpers/helpers';
import {
    UpstreamSubscription,
    UpstreamSubscriptionData,
    UpstreamSubscriptionsStatus,
    addSubscription,
    getStatus,
    refreshSubscriptions,
    removeSubscription,
    setConfig,
    setSubscription,
} from './api';
import { SubscriptionModal } from './Modal';

const getTitleForInterval = (interval: number) => {
    if (interval === 0) {
        return i18next.t('disabled');
    }

    if (interval % DAY_HOURS === 0) {
        return i18next.t('interval_days', { count: interval / DAY_HOURS });
    }

    return i18next.t('interval_hours', { count: interval });
};

type ModalState = {
    isOpen: boolean;
    subscription?: UpstreamSubscription;
};

const UpstreamSubscriptions = () => {
    const { t } = useTranslation();
    const dispatch = useDispatch();

    const [status, setStatus] = useState<UpstreamSubscriptionsStatus>({
        enabled: true,
        interval: 24,
        subscriptions: [],
    });
    const [loading, setLoading] = useState(true);
    const [processing, setProcessing] = useState(false);
    const [modal, setModal] = useState<ModalState>({ isOpen: false });

    const load = useCallback(async () => {
        try {
            const data = await getStatus();
            if (data) {
                setStatus({ ...data, subscriptions: data.subscriptions || [] });
            }
        } catch (error) {
            dispatch(addErrorToast({ error }));
        } finally {
            setLoading(false);
        }
    }, [dispatch]);

    useEffect(() => {
        load();
    }, [load]);

    /**
     * Runs a modifying request and reloads the status.  Returns true on
     * success.
     */
    const modify = async (request: () => Promise<unknown>, successMessage?: string) => {
        setProcessing(true);
        try {
            await request();
            if (successMessage) {
                dispatch(addSuccessToast(successMessage));
            }

            return true;
        } catch (error) {
            dispatch(addErrorToast({ error }));

            return false;
        } finally {
            await load();
            setProcessing(false);
        }
    };

    const handleSubmit = async (data: UpstreamSubscriptionData) => {
        const { subscription } = modal;
        const ok = subscription
            ? await modify(() => setSubscription(subscription.id, data))
            : await modify(
                  () => addSubscription(data),
                  t('upstream_subscription_added', { name: data.name || data.url }),
              );

        if (ok) {
            setModal({ isOpen: false });
        }
    };

    const handleToggle = (sub: UpstreamSubscription) =>
        modify(() =>
            setSubscription(sub.id, {
                name: sub.name,
                url: sub.url,
                upstreams: sub.upstreams,
                enabled: !sub.enabled,
            }),
        );

    const handleDelete = (sub: UpstreamSubscription) => {
        if (window.confirm(t('upstream_subscription_confirm_delete', { name: sub.name || sub.url }))) {
            modify(() => removeSubscription(sub.id));
        }
    };

    const handleRefresh = async (id?: number) => {
        setProcessing(true);
        try {
            const resp = await refreshSubscriptions(id);
            if (resp.errors?.length) {
                dispatch(addErrorToast({ error: resp.errors.join('\n') }));
            }

            if (resp.updated > 0) {
                dispatch(addSuccessToast(i18next.t('list_updated', { count: resp.updated })));
            } else if (!resp.errors?.length) {
                dispatch(addSuccessToast('all_lists_up_to_date_toast'));
            }
        } catch (error) {
            dispatch(addErrorToast({ error }));
        } finally {
            await load();
            setProcessing(false);
        }
    };

    const intervals = FILTERS_INTERVALS_HOURS.includes(status.interval)
        ? FILTERS_INTERVALS_HOURS
        : [...FILTERS_INTERVALS_HOURS, status.interval];

    const columns = [
        {
            Header: t('enabled_table_header'),
            accessor: 'enabled',
            width: 90,
            className: 'text-center',
            resizable: false,
            Cell: ({ original }: { original: UpstreamSubscription }) => (
                <label className="checkbox">
                    <input
                        type="checkbox"
                        className="checkbox__input"
                        onChange={() => handleToggle(original)}
                        checked={original.enabled}
                        disabled={processing}
                    />

                    <span className="checkbox__label" />
                </label>
            ),
        },
        {
            Header: t('name_table_header'),
            accessor: 'name',
            minWidth: 140,
            Cell: CellWrap,
        },
        {
            Header: t('list_url_table_header'),
            accessor: 'url',
            minWidth: 180,
            Cell: ({ value }: { value: string }) => (
                <div className="logs__row">
                    <a href={value} target="_blank" rel="noopener noreferrer" className="link logs__text" title={value}>
                        {value}
                    </a>
                </div>
            ),
        },
        {
            Header: t('upstream_subscription_upstreams_header'),
            accessor: 'upstreams',
            minWidth: 160,
            Cell: ({ value }: { value: string[] }) =>
                CellWrap({
                    value: value?.length ? value.join(', ') : t('upstream_subscription_upstreams_from_list'),
                }),
        },
        {
            Header: t('rules_count_table_header'),
            accessor: 'rules_count',
            className: 'text-center',
            minWidth: 100,
            Cell: ({ value, original }: { value: number; original: UpstreamSubscription }) => (
                <span
                    title={
                        original.invalid_count
                            ? t('upstream_subscription_invalid_lines', { count: original.invalid_count })
                            : undefined
                    }>
                    {(value || 0).toLocaleString()}
                </span>
            ),
        },
        {
            Header: t('last_time_updated_table_header'),
            accessor: 'last_updated',
            className: 'text-center',
            minWidth: 160,
            Cell: (row: { value?: string; original: UpstreamSubscription }) =>
                row.original.last_error ? (
                    <span className="text-danger" title={row.original.last_error}>
                        {t('upstream_subscription_update_failed')}
                    </span>
                ) : (
                    CellWrap(row, formatDetailedDateTime)
                ),
        },
        {
            Header: t('actions_table_header'),
            accessor: 'id',
            className: 'text-center',
            width: 140,
            sortable: false,
            resizable: false,
            Cell: ({ original }: { original: UpstreamSubscription }) => (
                <div className="logs__row logs__row--center">
                    <button
                        type="button"
                        className="btn btn-icon btn-outline-primary btn-sm mr-2"
                        title={t('check_updates_btn')}
                        disabled={processing || !original.enabled}
                        onClick={() => handleRefresh(original.id)}>
                        <svg className="icons icon12">
                            <use xlinkHref="#refresh" />
                        </svg>
                    </button>

                    <button
                        type="button"
                        className="btn btn-icon btn-outline-primary btn-sm mr-2"
                        title={t('edit_table_action')}
                        disabled={processing}
                        onClick={() => setModal({ isOpen: true, subscription: original })}>
                        <svg className="icons icon12">
                            <use xlinkHref="#edit" />
                        </svg>
                    </button>

                    <button
                        type="button"
                        className="btn btn-icon btn-outline-secondary btn-sm"
                        title={t('delete_table_action')}
                        disabled={processing}
                        onClick={() => handleDelete(original)}>
                        <svg className="icons icon12">
                            <use xlinkHref="#delete" />
                        </svg>
                    </button>
                </div>
            ),
        },
    ];

    return (
        <Card
            id="upstream_subscriptions"
            title={t('upstream_subscriptions')}
            subtitle={t('upstream_subscriptions_desc')}
            bodyType="card-body box-body--settings">
            <div className="row">
                <div className="col-12">
                    <div className="form__group form__group--settings">
                        <Checkbox
                            data-testid="upstream_subscriptions_enabled"
                            title={t('upstream_subscriptions_enabled')}
                            subtitle={t('upstream_subscriptions_enabled_desc')}
                            value={status.enabled}
                            disabled={processing || loading}
                            onChange={(enabled) => modify(() => setConfig({ enabled }))}
                        />
                    </div>
                </div>

                <div className="col-12 col-md-5">
                    <div className="form__group form__group--inner mb-4">
                        <label className="form__label">{t('upstream_subscriptions_interval')}</label>

                        <Select
                            data-testid="upstream_subscriptions_interval"
                            value={status.interval}
                            disabled={processing || loading}
                            onChange={(e) => {
                                const interval = Number(e.target.value);
                                modify(() => setConfig({ interval }));
                            }}>
                            {intervals.map((interval) => (
                                <option value={interval} key={interval}>
                                    {getTitleForInterval(interval)}
                                </option>
                            ))}
                        </Select>
                    </div>
                </div>

                <div className="col-12">
                    <div className="form__desc mb-4">{t('upstream_subscriptions_formats')}</div>

                    <ReactTable
                        data={status.subscriptions}
                        columns={columns}
                        showPagination={false}
                        pageSize={Math.max(status.subscriptions.length, 3)}
                        loading={loading}
                        minRows={3}
                        loadingText={t('loading_table_status')}
                        noDataText={t('upstream_subscriptions_empty')}
                    />

                    <div className="card-actions mt-4">
                        <button
                            className="btn btn-success btn-standard mr-2 btn-large mb-2"
                            type="button"
                            disabled={processing}
                            onClick={() => setModal({ isOpen: true })}>
                            {t('upstream_subscription_add')}
                        </button>

                        <button
                            className="btn btn-primary btn-standard mb-2"
                            type="button"
                            disabled={processing}
                            onClick={() => handleRefresh()}>
                            {t('check_updates_btn')}
                        </button>
                    </div>
                </div>
            </div>

            <SubscriptionModal
                isOpen={modal.isOpen}
                subscription={modal.subscription}
                processing={processing}
                onClose={() => setModal({ isOpen: false })}
                onSubmit={handleSubmit}
            />
        </Card>
    );
};

export default UpstreamSubscriptions;

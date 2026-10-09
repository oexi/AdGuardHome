import React from 'react';
import ReactModal from 'react-modal';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';

import { Input } from '../../../ui/Controls/Input';
import { Textarea } from '../../../ui/Controls/Textarea';
import { validateRequiredValue } from '../../../../helpers/validators';
import { UpstreamSubscription, UpstreamSubscriptionData } from './api';
import '../../../ui/Modal.css';

const R_HTTP_URL = /^https?:\/\/[^\s/]+/i;

type FormValues = {
    name: string;
    url: string;
    upstreams: string;
};

type Props = {
    isOpen: boolean;
    subscription?: UpstreamSubscription;
    processing: boolean;
    onClose: () => void;
    onSubmit: (data: UpstreamSubscriptionData) => void;
};

const splitUpstreams = (text: string) =>
    text
        .split('\n')
        .map((line) => line.trim())
        .filter((line) => line && !line.startsWith('#'));

const SubscriptionForm = ({ subscription, processing, onClose, onSubmit }: Omit<Props, 'isOpen'>) => {
    const { t } = useTranslation();

    const { handleSubmit, control } = useForm<FormValues>({
        mode: 'onBlur',
        defaultValues: {
            name: subscription?.name || '',
            url: subscription?.url || '',
            upstreams: subscription?.upstreams?.join('\n') || '',
        },
    });

    const validateUrl = (value: string) =>
        R_HTTP_URL.test(value.trim()) ? undefined : t('upstream_subscription_url_error');

    const submit = (values: FormValues) => {
        onSubmit({
            name: values.name.trim(),
            url: values.url.trim(),
            upstreams: splitUpstreams(values.upstreams),
            enabled: subscription ? subscription.enabled : true,
        });
    };

    return (
        <form onSubmit={handleSubmit(submit)}>
            <div className="modal-body">
                <div className="form__group">
                    <Controller
                        name="name"
                        control={control}
                        render={({ field }) => (
                            <Input
                                {...field}
                                type="text"
                                data-testid="upstream_subscription_name"
                                placeholder={t('enter_name_hint')}
                                trimOnBlur
                            />
                        )}
                    />
                </div>

                <div className="form__group">
                    <Controller
                        name="url"
                        control={control}
                        rules={{ validate: { validateRequiredValue, validateUrl } }}
                        render={({ field, fieldState }) => (
                            <Input
                                {...field}
                                type="text"
                                data-testid="upstream_subscription_url"
                                placeholder={t('upstream_subscription_url_hint')}
                                error={fieldState.error?.message}
                                trimOnBlur
                            />
                        )}
                    />
                </div>

                <div className="form__group">
                    <Controller
                        name="upstreams"
                        control={control}
                        render={({ field }) => (
                            <Textarea
                                {...field}
                                data-testid="upstream_subscription_upstreams"
                                label={t('upstream_subscription_upstreams_label')}
                                desc={t('upstream_subscription_upstreams_desc')}
                                placeholder="https://dns.alidns.com/dns-query"
                                trimOnBlur
                            />
                        )}
                    />
                </div>

                <div className="form__description">{t('upstream_subscription_modal_desc')}</div>
            </div>

            <div className="modal-footer">
                <button type="button" className="btn btn-secondary" onClick={onClose}>
                    {t('cancel_btn')}
                </button>

                <button
                    type="submit"
                    data-testid="upstream_subscription_save"
                    className="btn btn-success"
                    disabled={processing}>
                    {t('save_btn')}
                </button>
            </div>
        </form>
    );
};

export const SubscriptionModal = ({ isOpen, subscription, processing, onClose, onSubmit }: Props) => {
    const { t } = useTranslation();

    return (
        <ReactModal
            className="Modal__Bootstrap modal-dialog modal-dialog-centered"
            closeTimeoutMS={0}
            isOpen={isOpen}
            onRequestClose={onClose}>
            <div className="modal-content">
                <div className="modal-header">
                    <h4 className="modal-title">
                        {subscription ? t('upstream_subscription_edit') : t('upstream_subscription_add')}
                    </h4>

                    <button type="button" className="close" onClick={onClose}>
                        <span className="sr-only">Close</span>
                    </button>
                </div>

                {isOpen && (
                    <SubscriptionForm
                        subscription={subscription}
                        processing={processing}
                        onClose={onClose}
                        onSubmit={onSubmit}
                    />
                )}
            </div>
        </ReactModal>
    );
};

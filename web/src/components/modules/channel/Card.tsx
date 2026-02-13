import { useState } from 'react';
import {
    MorphingDialog,
    MorphingDialogTrigger,
    MorphingDialogContainer,
    MorphingDialogContent,
} from '@/components/ui/morphing-dialog';
import { Copy, MessageSquare } from 'lucide-react';
import { ChannelType, type Channel, useCreateChannel, useEnableChannel } from '@/api/endpoints/channel';
import { type StatsMetricsFormatted } from '@/api/endpoints/stats';
import { CardContent } from './CardContent';
import { ChannelForm, type ChannelFormData } from './Form';
import { useTranslations } from 'next-intl';
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/animate-ui/components/animate/tooltip';
import { Switch } from '@/components/ui/switch';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { toast } from '@/components/common/Toast';

const MAX_MODEL_PREVIEW_COUNT = 3;

function getDefaultCloneName(name: string): string {
    const trimmed = name.trim();
    if (!trimmed) return 'copy';

    const copyPattern = /^(.*)-copy(?:-(\d+))?$/;
    const matched = trimmed.match(copyPattern);
    if (!matched) {
        return `${trimmed}-copy`;
    }

    const baseName = matched[1];
    const index = matched[2] ? Number(matched[2]) + 1 : 2;
    return `${baseName}-copy-${index}`;
}

function toCloneFormData(channel: Channel): ChannelFormData {
    return {
        name: getDefaultCloneName(channel.name),
        type: channel.type,
        enabled: channel.enabled,
        base_urls: channel.base_urls?.length ? channel.base_urls.map((u) => ({ ...u })) : [{ url: '', delay: 0 }],
        custom_header: channel.custom_header?.length ? channel.custom_header.map((h) => ({ ...h })) : [],
        channel_proxy: channel.channel_proxy ?? '',
        param_override: channel.param_override ?? '',
        keys: channel.keys.length > 0
            ? channel.keys.map((k) => ({
                enabled: k.enabled,
                channel_key: k.channel_key,
                remark: k.remark ?? '',
            }))
            : [{ enabled: true, channel_key: '', remark: '' }],
        model: channel.model,
        custom_model: channel.custom_model,
        proxy: channel.proxy,
        auto_sync: channel.auto_sync,
        auto_group: channel.auto_group,
        match_regex: channel.match_regex ?? '',
    };
}

export function Card({ channel, stats }: { channel: Channel; stats: StatsMetricsFormatted }) {
    const t = useTranslations('channel.card');
    const tForm = useTranslations('channel.form');
    const tDetail = useTranslations('channel.detail');
    const tCreate = useTranslations('channel.create');
    const enableChannel = useEnableChannel();
    const createChannel = useCreateChannel();

    const [isCloneDialogOpen, setIsCloneDialogOpen] = useState(false);
    const [cloneFormData, setCloneFormData] = useState<ChannelFormData>(() => toCloneFormData(channel));

    const handleEnableChange = (checked: boolean) => {
        enableChannel.mutate(
            { id: channel.id, enabled: checked },
            {
                onSuccess: () => {
                    toast.success(checked ? t('toast.enabled') : t('toast.disabled'));
                },
                onError: (error) => {
                    toast.error(error.message);
                },
            }
        );
    };

    const handleOpenCloneDialog = (event: React.MouseEvent<HTMLButtonElement>) => {
        event.stopPropagation();
        setCloneFormData(toCloneFormData(channel));
        setIsCloneDialogOpen(true);
    };

    const handleCloneSubmit = (event: React.FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        const normalizedBaseUrls = (cloneFormData.base_urls ?? []).filter((u) => u.url.trim()).map((u) => ({
            url: u.url.trim(),
            delay: Number(u.delay || 0),
        }));
        const normalizedKeys = cloneFormData.keys
            .filter((k) => k.channel_key.trim())
            .map((k) => ({ enabled: k.enabled, channel_key: k.channel_key.trim(), remark: k.remark ?? '' }));
        const normalizedHeaders = (cloneFormData.custom_header ?? [])
            .map((h) => ({ header_key: h.header_key.trim(), header_value: h.header_value }))
            .filter((h) => h.header_key);

        createChannel.mutate(
            {
                name: cloneFormData.name,
                type: cloneFormData.type,
                enabled: cloneFormData.enabled,
                base_urls: normalizedBaseUrls,
                keys: normalizedKeys,
                model: cloneFormData.model,
                custom_model: cloneFormData.custom_model,
                proxy: cloneFormData.proxy,
                auto_sync: cloneFormData.auto_sync,
                auto_group: cloneFormData.auto_group,
                custom_header: normalizedHeaders,
                channel_proxy: cloneFormData.channel_proxy.trim(),
                param_override: cloneFormData.param_override.trim(),
                match_regex: cloneFormData.match_regex.trim(),
            },
            {
                onSuccess: () => {
                    setIsCloneDialogOpen(false);
                },
                onError: (error) => {
                    toast.error(error.message);
                },
            }
        );
    };

    const typeLabelMap: Record<ChannelType, string> = {
        [ChannelType.OpenAIChat]: tForm('typeOpenAIChat'),
        [ChannelType.OpenAIResponse]: tForm('typeOpenAIResponse'),
        [ChannelType.Anthropic]: tForm('typeAnthropic'),
        [ChannelType.Gemini]: tForm('typeGemini'),
        [ChannelType.Volcengine]: tForm('typeVolcengine'),
        [ChannelType.OpenAIEmbedding]: tForm('typeOpenAIEmbedding'),
    };

    const autoModels = channel.model
        ? channel.model.split(',').map((m) => m.trim()).filter(Boolean)
        : [];
    const customModels = channel.custom_model
        ? channel.custom_model.split(',').map((m) => m.trim()).filter(Boolean)
        : [];
    const mergedModels = Array.from(new Set([...autoModels, ...customModels]));
    const previewModels = mergedModels.slice(0, MAX_MODEL_PREVIEW_COUNT);
    const modelPreviewText = previewModels.join(', ');
    const hasMoreModels = mergedModels.length > MAX_MODEL_PREVIEW_COUNT;
    const fullModelText = mergedModels.join(', ');

    return (
        <>
            <MorphingDialog>
                <MorphingDialogTrigger className="w-full">
                    <article className="relative flex h-54 flex-col justify-between gap-2.5 rounded-3xl border border-border bg-card text-card-foreground p-4 custom-shadow transition-all duration-300 hover:scale-[1.02]">
                        <header className="relative flex items-center justify-between gap-2">
                            <Tooltip side="top" sideOffset={10} align="center">
                                <TooltipTrigger asChild>
                                    <h3 className="text-lg font-bold truncate min-w-0">{channel.name}</h3>
                                </TooltipTrigger>
                                <TooltipContent key={channel.name}>{channel.name}</TooltipContent>
                            </Tooltip>
                            <div className="flex items-center gap-1 shrink-0">
                                <Tooltip side="top" sideOffset={10} align="center">
                                    <TooltipTrigger asChild>
                                        <button
                                            type="button"
                                            onClick={handleOpenCloneDialog}
                                            className="p-1.5 rounded-lg transition-colors hover:bg-muted text-muted-foreground hover:text-foreground"
                                        >
                                            <Copy className="size-4" />
                                        </button>
                                    </TooltipTrigger>
                                    <TooltipContent>{tCreate('submit')}</TooltipContent>
                                </Tooltip>
                                <Switch
                                    checked={channel.enabled}
                                    onCheckedChange={handleEnableChange}
                                    disabled={enableChannel.isPending}
                                    onClick={(e) => e.stopPropagation()}
                                />
                            </div>
                        </header>

                        <div className="space-y-0.5">
                            <div className="flex items-center gap-1.5 text-xs min-w-0">
                                <span className="shrink-0 text-muted-foreground">{t('type')}:</span>
                                <span className="truncate text-card-foreground">{typeLabelMap[channel.type]}</span>
                            </div>
                            <div className="flex items-center gap-1.5 text-xs min-w-0">
                                <span className="shrink-0 text-muted-foreground">{t('model')}:</span>
                                {mergedModels.length > 0 ? (
                                    <Tooltip side="top" sideOffset={10} align="center">
                                        <TooltipTrigger asChild>
                                            <span className="truncate text-card-foreground">
                                                {modelPreviewText}
                                                {hasMoreModels ? ' ...' : ''}
                                            </span>
                                        </TooltipTrigger>
                                        <TooltipContent key={fullModelText} className="max-w-sm break-all">{fullModelText}</TooltipContent>
                                    </Tooltip>
                                ) : (
                                    <span className="truncate text-card-foreground">{t('noModels')}</span>
                                )}
                            </div>
                        </div>

                        <dl className="relative grid grid-cols-1 gap-2">
                            <div className="flex items-center justify-between rounded-2xl border border-border/70 bg-background/80 px-2.5 py-2">
                                <div className="flex min-w-0 items-center gap-2.5">
                                    <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                                        <MessageSquare className="h-4 w-4" />
                                    </span>
                                    <dt className="truncate text-sm text-muted-foreground">{t('requestCount')}</dt>
                                </div>
                                <dd className="shrink-0 text-sm font-medium text-card-foreground">
                                    {stats.request_count.formatted.value}
                                    <span className="ml-1 text-xs text-muted-foreground">{stats.request_count.formatted.unit}</span>
                                </dd>
                            </div>
                        </dl>
                    </article>
                </MorphingDialogTrigger>

                <MorphingDialogContainer>
                    <MorphingDialogContent
                        className="w-full md:max-w-xl bg-card text-card-foreground px-4 py-2 custom-shadow rounded-3xl max-h-[90vh] overflow-y-auto"
                        nestedDialogOwnerId={`channel-detail-${channel.id}`}
                    >
                        <CardContent channel={channel} stats={stats} />
                    </MorphingDialogContent>
                </MorphingDialogContainer>
            </MorphingDialog>

            <Dialog open={isCloneDialogOpen} onOpenChange={setIsCloneDialogOpen}>
                <DialogContent className="sm:max-w-xl max-h-[90vh] overflow-y-auto">
                    <DialogHeader>
                        <DialogTitle>{tCreate('dialogTitle')}</DialogTitle>
                    </DialogHeader>
                    <ChannelForm
                        formData={cloneFormData}
                        onFormDataChange={setCloneFormData}
                        onSubmit={handleCloneSubmit}
                        isPending={createChannel.isPending}
                        submitText={tCreate('submit')}
                        pendingText={tCreate('submitting')}
                        onCancel={() => setIsCloneDialogOpen(false)}
                        cancelText={tDetail('actions.cancel')}
                        idPrefix={`clone-channel-${channel.id}`}
                        nestedDialogOwnerId={`channel-clone-${channel.id}`}
                    />
                </DialogContent>
            </Dialog>
        </>
    );
}

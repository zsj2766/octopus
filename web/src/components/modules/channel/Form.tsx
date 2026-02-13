import { AutoGroupType, ChannelType, type Channel, useFetchModel } from '@/api/endpoints/channel';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { toast } from '@/components/common/Toast';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';
import { useTranslations } from 'next-intl';
import { useEffect, useMemo, useRef, useState } from 'react';
import { Check, HelpCircle, RefreshCw, Search, X, Plus } from 'lucide-react';

export interface ChannelKeyFormItem {
    id?: number;
    enabled: boolean;
    channel_key: string;
    status_code?: number;
    last_use_time_stamp?: number;
    total_cost?: number;
    remark?: string;
}

export interface ChannelFormData {
    name: string;
    type: ChannelType;
    base_urls: Channel['base_urls'];
    custom_header: Channel['custom_header'];
    channel_proxy: string;
    param_override: string;
    keys: ChannelKeyFormItem[];
    model: string;
    custom_model: string;
    enabled: boolean;
    proxy: boolean;
    auto_sync: boolean;
    auto_group: AutoGroupType;
    match_regex: string;
}

export interface ChannelFormProps {
    formData: ChannelFormData;
    onFormDataChange: (data: ChannelFormData) => void;
    onSubmit: (event: React.FormEvent<HTMLFormElement>) => void;
    isPending: boolean;
    submitText: string;
    pendingText: string;
    onCancel?: () => void;
    cancelText?: string;
    idPrefix?: string;
    nestedDialogOwnerId?: string;
}

import {
    Accordion,
    AccordionContent,
    AccordionItem,
    AccordionTrigger,
} from "@/components/ui/accordion";

export function ChannelForm({
    formData,
    onFormDataChange,
    onSubmit,
    isPending,
    submitText,
    pendingText,
    onCancel,
    cancelText,
    idPrefix = 'channel',
    nestedDialogOwnerId,
}: ChannelFormProps) {
    const t = useTranslations('channel.form');

    // Ensure the form always shows at least 1 row for base_urls / keys / custom_header.
    // This avoids "empty list" UI and also keeps URL + APIKEY layout consistent.
    useEffect(() => {
        if (!formData.base_urls || formData.base_urls.length === 0) {
            onFormDataChange({ ...formData, base_urls: [{ url: '', delay: 0 }] });
            return;
        }
        if (!formData.keys || formData.keys.length === 0) {
            onFormDataChange({ ...formData, keys: [{ enabled: true, channel_key: '' }] });
            return;
        }
        if (!formData.custom_header || formData.custom_header.length === 0) {
            onFormDataChange({ ...formData, custom_header: [{ header_key: '', header_value: '' }] });
        }
    }, [formData, onFormDataChange]);

    const autoModels = formData.model
        ? formData.model.split(',').map((m) => m.trim()).filter(Boolean)
        : [];
    const customModels = formData.custom_model
        ? formData.custom_model.split(',').map((m) => m.trim()).filter(Boolean)
        : [];
    const [inputValue, setInputValue] = useState('');
    const [fetchedModels, setFetchedModels] = useState<string[]>([]);
    const [selectedFetchedModels, setSelectedFetchedModels] = useState<string[]>([]);
    const [fetchedModelsSearch, setFetchedModelsSearch] = useState('');
    const [showFetchedModelsDialog, setShowFetchedModelsDialog] = useState(false);
    const inputRef = useRef<HTMLInputElement>(null);

    const fetchModel = useFetchModel();

    const effectiveKey =
        formData.keys.find((k) => k.enabled && k.channel_key.trim())?.channel_key.trim() || '';

    const fetchModelPayload = useMemo(() => ({
        name: formData.name.trim(),
        type: formData.type,
        base_urls: formData.base_urls,
        keys: formData.keys
            .filter((k) => k.channel_key.trim())
            .map((k) => ({ enabled: k.enabled, channel_key: k.channel_key.trim() })),
        proxy: formData.proxy,
        channel_proxy: formData.channel_proxy.trim() || null,
        match_regex: formData.match_regex.trim() || null,
        custom_header: (formData.custom_header ?? [])
            .map((h) => ({
                header_key: h.header_key.trim(),
                header_value: h.header_value,
            }))
            .filter((h) => h.header_key),
    }), [
        formData.name,
        formData.type,
        formData.base_urls,
        formData.keys,
        formData.proxy,
        formData.channel_proxy,
        formData.match_regex,
        formData.custom_header,
    ]);

    const updateModels = (nextAuto: string[], nextCustom: string[]) => {
        const model = nextAuto.join(',');
        const custom_model = nextCustom.join(',');
        if (formData.model === model && formData.custom_model === custom_model) return;
        onFormDataChange({ ...formData, model, custom_model });
    };

    const normalizeFetchedModelIds = (payload: unknown): string[] => {
        const normalizeModelItem = (item: unknown) => {
            if (typeof item === 'string') return item.trim();
            if (item && typeof item === 'object' && 'id' in item) {
                const id = (item as { id?: unknown }).id;
                return typeof id === 'string' ? id.trim() : '';
            }
            return '';
        };

        if (Array.isArray(payload)) {
            return Array.from(new Set(payload
                .map(normalizeModelItem)
                .filter(Boolean)));
        }

        if (payload && typeof payload === 'object' && 'data' in payload) {
            const list = (payload as { data?: unknown }).data;
            if (!Array.isArray(list)) return [];
            return Array.from(new Set(list
                .map(normalizeModelItem)
                .filter(Boolean)));
        }

        return [];
    };

    const handleRefreshModels = () => {
        if (!formData.base_urls?.[0]?.url || !effectiveKey) return;
        fetchModel.mutate(
            fetchModelPayload,
            {
                onSuccess: (data) => {
                    const fetchedModelIds = normalizeFetchedModelIds(data);
                    if (fetchedModelIds.length > 0) {
                        const nextAuto = Array.from(new Set([...autoModels, ...fetchedModelIds].map((m) => m.trim()).filter(Boolean)));
                        updateModels(nextAuto, customModels);
                        toast.success(t('modelRefreshSuccess'), { duration: 2500, position: 'top-right' });
                    } else {
                        toast.warning(t('modelRefreshEmpty'));
                    }
                },
                onError: (error) => {
                    const errorMessage = (error && typeof error === 'object' && 'message' in error)
                        ? String((error as { message?: unknown }).message ?? '')
                        : String(error);
                    toast.error(t('modelRefreshFailed'), { description: errorMessage || t('modelRefreshEmpty') });
                },
            }
        );
    };

    const handleSelectModels = () => {
        if (!formData.base_urls?.[0]?.url || !effectiveKey) return;
        fetchModel.mutate(
            fetchModelPayload,
            {
                onSuccess: (data) => {
                    const fetchedModelIds = normalizeFetchedModelIds(data);
                    if (fetchedModelIds.length > 0) {
                        setFetchedModels(fetchedModelIds);
                        setSelectedFetchedModels([]);
                        setFetchedModelsSearch('');
                        setShowFetchedModelsDialog(true);
                        toast.success(t('modelRefreshSuccess'), { duration: 2500, position: 'top-right' });
                    } else {
                        toast.warning(t('modelRefreshEmpty'));
                    }
                },
                onError: (error) => {
                    const errorMessage = (error && typeof error === 'object' && 'message' in error)
                        ? String((error as { message?: unknown }).message ?? '')
                        : String(error);
                    toast.error(t('modelRefreshFailed'), { description: errorMessage || t('modelRefreshEmpty') });
                },
            }
        );
    };

    const toggleFetchedModelSelection = (model: string) => {
        setSelectedFetchedModels((prev) =>
            prev.includes(model)
                ? prev.filter((m) => m !== model)
                : [...prev, model]
        );
    };

    const handleConfirmFetchedModels = () => {
        if (selectedFetchedModels.length === 0) {
            setShowFetchedModelsDialog(false);
            return;
        }
        const nextAuto = Array.from(new Set([...autoModels, ...selectedFetchedModels].map((m) => m.trim()).filter(Boolean)));
        updateModels(nextAuto, customModels);
        setShowFetchedModelsDialog(false);
        setSelectedFetchedModels([]);
    };

    const filteredFetchedModels = fetchedModelsSearch.trim()
        ? fetchedModels.filter((model) => model.toLowerCase().includes(fetchedModelsSearch.trim().toLowerCase()))
        : fetchedModels;

    const isTranslationKey = (value: string, key: string) => (
        value === key || value === `channel.form.${key}`
    );

    const modelSelectorConfirmTranslated = t('modelSelectorConfirm', { count: selectedFetchedModels.length });
    const modelSelectorConfirmEmptyTranslated = t('modelSelectorConfirmEmpty');
    const modelAddTranslated = t('modelAdd');

    const modelSelectorConfirmLabel = selectedFetchedModels.length > 0
        ? (!isTranslationKey(modelSelectorConfirmTranslated, 'modelSelectorConfirm')
            ? modelSelectorConfirmTranslated
            : !isTranslationKey(modelSelectorConfirmEmptyTranslated, 'modelSelectorConfirmEmpty')
                ? `${modelSelectorConfirmEmptyTranslated} (${selectedFetchedModels.length})`
                : modelAddTranslated)
        : (!isTranslationKey(modelSelectorConfirmEmptyTranslated, 'modelSelectorConfirmEmpty')
            ? modelSelectorConfirmEmptyTranslated
            : modelAddTranslated);

    const handleAddModel = (model: string) => {
        const trimmedModel = model.trim();
        if (trimmedModel && !customModels.includes(trimmedModel) && !autoModels.includes(trimmedModel)) {
            updateModels(autoModels, [...customModels, trimmedModel]);
        }
        setInputValue('');
    };

    const handleRemoveAutoModel = (model: string) => {
        updateModels(autoModels.filter(m => m !== model), customModels);
    };

    const handleRemoveCustomModel = (model: string) => {
        updateModels(autoModels, customModels.filter(m => m !== model));
    };

    const handleInputKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            if (inputValue.trim()) handleAddModel(inputValue);
        }
    };

    const handleAddKey = () => {
        onFormDataChange({
            ...formData,
            keys: [...formData.keys, { enabled: true, channel_key: '' }],
        });
    };

    const handleUpdateKey = (idx: number, patch: Partial<ChannelKeyFormItem>) => {
        const next = formData.keys.map((k, i) => (i === idx ? { ...k, ...patch } : k));
        onFormDataChange({ ...formData, keys: next });
    };

    const handleRemoveKey = (idx: number) => {
        const curr = formData.keys ?? [];
        if (curr.length <= 1) return;
        const next = curr.filter((_, i) => i !== idx);
        onFormDataChange({ ...formData, keys: next });
    };

    const handleAddBaseUrl = () => {
        onFormDataChange({
            ...formData,
            base_urls: [...(formData.base_urls ?? []), { url: '', delay: 0 }],
        });
    };

    const handleUpdateBaseUrl = (idx: number, patch: Partial<Channel['base_urls'][number]>) => {
        const next = (formData.base_urls ?? []).map((u, i) => (i === idx ? { ...u, ...patch } : u));
        onFormDataChange({ ...formData, base_urls: next });
    };

    const handleRemoveBaseUrl = (idx: number) => {
        const curr = formData.base_urls ?? [];
        if (curr.length <= 1) return;
        onFormDataChange({ ...formData, base_urls: curr.filter((_, i) => i !== idx) });
    };

    const handleAddHeader = () => {
        onFormDataChange({
            ...formData,
            custom_header: [...(formData.custom_header ?? []), { header_key: '', header_value: '' }],
        });
    };

    const handleUpdateHeader = (idx: number, patch: Partial<Channel['custom_header'][number]>) => {
        const next = (formData.custom_header ?? []).map((h, i) => (i === idx ? { ...h, ...patch } : h));
        onFormDataChange({ ...formData, custom_header: next });
    };

    const handleRemoveHeader = (idx: number) => {
        const curr = formData.custom_header ?? [];
        if (curr.length <= 1) return;
        onFormDataChange({ ...formData, custom_header: curr.filter((_, i) => i !== idx) });
    };

    return (
        <form onSubmit={onSubmit} className="space-y-4 px-1">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                    <label htmlFor={`${idPrefix}-name`} className="text-sm font-medium text-card-foreground">
                        {t('name')}
                    </label>
                    <Input
                        className='rounded-xl'
                        id={`${idPrefix}-name`}
                        type="text"
                        value={formData.name}
                        onChange={(event) => onFormDataChange({ ...formData, name: event.target.value })}
                        required
                    />
                </div>

                <div className="space-y-2">
                    <label htmlFor={`${idPrefix}-type`} className="text-sm font-medium text-card-foreground">
                        {t('type')}
                    </label>
                    <Select
                        value={String(formData.type)}
                        onValueChange={(value) => onFormDataChange({ ...formData, type: Number(value) as ChannelType })}
                    >
                        <SelectTrigger id={`${idPrefix}-type`} className="rounded-xl w-full border border-border px-4 py-2 text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent className='rounded-xl'>
                            <SelectItem className='rounded-xl' value={String(ChannelType.OpenAIChat)}>{t('typeOpenAIChat')}</SelectItem>
                            <SelectItem className='rounded-xl' value={String(ChannelType.OpenAIResponse)}>{t('typeOpenAIResponse')}</SelectItem>
                            <SelectItem className='rounded-xl' value={String(ChannelType.Anthropic)}>{t('typeAnthropic')}</SelectItem>
                            <SelectItem className='rounded-xl' value={String(ChannelType.Gemini)}>{t('typeGemini')}</SelectItem>
                            <SelectItem className='rounded-xl' value={String(ChannelType.Volcengine)}>{t('typeVolcengine')}</SelectItem>
                            <SelectItem className='rounded-xl' value={String(ChannelType.OpenAIEmbedding)}>{t('typeOpenAIEmbedding')}</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
            </div>

            <div className="space-y-2">
                <div className="flex items-center justify-between">
                    <label className="text-sm font-medium text-card-foreground">
                        {t('baseUrls')} {formData.base_urls.length > 0 ? `(${formData.base_urls.length})` : ''}
                    </label>
                    <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={handleAddBaseUrl}
                        className="h-6 px-2 text-xs text-muted-foreground/70 hover:text-muted-foreground hover:bg-transparent"
                    >
                        <Plus className="h-3 w-3 mr-1" />
                        {t('add')}
                    </Button>
                </div>
                <div className="space-y-2">
                    {(formData.base_urls ?? []).map((u, idx) => (
                        <div key={`baseurl-${idx}`} className="flex items-center gap-2">
                            <Input
                                id={`${idPrefix}-base-${idx}`}
                                type="url"
                                value={u.url}
                                onChange={(e) => handleUpdateBaseUrl(idx, { url: e.target.value })}
                                placeholder={t('baseUrlUrl')}
                                required={idx === 0}
                                className="rounded-xl flex-1"
                            />
                            <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                onClick={() => handleRemoveBaseUrl(idx)}
                                disabled={(formData.base_urls ?? []).length <= 1}
                                className="h-8 w-8 p-0 rounded-xl text-muted-foreground hover:text-destructive disabled:opacity-40 hover:bg-transparent"
                                title="Remove"
                            >
                                <X className="h-4 w-4" />
                            </Button>
                        </div>
                    ))}
                </div>
            </div>

            <div className="space-y-2">
                <div className="flex items-center justify-between">
                    <label className="text-sm font-medium text-card-foreground">
                        {t('apiKey')} {formData.keys.length > 0 ? `(${formData.keys.length})` : ''}
                    </label>
                    <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={handleAddKey}
                        className="h-6 px-2 text-xs text-muted-foreground/70 hover:text-muted-foreground hover:bg-transparent"
                    >
                        <Plus className="h-3 w-3 mr-1" />
                        {t('add')}
                    </Button>
                </div>
                <div className="space-y-2">
                    {(formData.keys ?? []).map((k, idx) => (
                        <div key={k.id ?? `new-${idx}`} className="flex items-center gap-2">
                            <Input
                                type="text"
                                value={k.channel_key}
                                onChange={(e) => handleUpdateKey(idx, { channel_key: e.target.value })}
                                placeholder={t('apiKey')}
                                required={idx === 0}
                                className="rounded-xl flex-1"
                            />
                            <Input
                                type="text"
                                value={k.remark ?? ''}
                                onChange={(e) => handleUpdateKey(idx, { remark: e.target.value })}
                                placeholder={t('remark')}
                                className="rounded-xl w-32"
                            />
                            <Switch
                                checked={k.enabled}
                                onCheckedChange={(checked) => handleUpdateKey(idx, { enabled: checked })}
                            />
                            <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                onClick={() => handleRemoveKey(idx)}
                                disabled={(formData.keys ?? []).length <= 1}
                                className="h-8 w-8 p-0 rounded-xl text-muted-foreground hover:text-destructive hover:bg-transparent disabled:opacity-40"
                                title="Remove"
                            >
                                <X className="h-4 w-4" />
                            </Button>
                        </div>
                    ))}
                </div>
            </div>

            <div className="space-y-2">
                <div className="flex items-center justify-between">
                    <label className="text-sm font-medium text-card-foreground">{t('model')}</label>
                    <div className="flex items-center gap-2">
                        <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            onClick={handleSelectModels}
                            disabled={!formData.base_urls?.[0]?.url || !effectiveKey || fetchModel.isPending}
                            className="h-6 px-2 text-xs text-muted-foreground/50 hover:text-muted-foreground hover:bg-transparent"
                        >
                            <Search className="h-3 w-3 mr-1" />
                            {t('modelSelect')}
                        </Button>
                        <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            onClick={handleRefreshModels}
                            disabled={!formData.base_urls?.[0]?.url || !effectiveKey || fetchModel.isPending}
                            className="h-6 px-2 text-xs text-muted-foreground/50 hover:text-muted-foreground hover:bg-transparent"
                        >
                            <RefreshCw className={`h-3 w-3 mr-1 ${fetchModel.isPending ? 'animate-spin' : ''}`} />
                            {t('modelRefresh')}
                        </Button>
                    </div>
                </div>
                <input type="hidden" value={formData.model} required />

                <Dialog
                    open={showFetchedModelsDialog}
                    onOpenChange={(open) => {
                        setShowFetchedModelsDialog(open);
                        if (!open) {
                            setSelectedFetchedModels([]);
                            setFetchedModelsSearch('');
                        }
                    }}
                >
                    <DialogContent
                        data-morphing-dialog-owner={nestedDialogOwnerId}
                        className="sm:max-w-2xl max-h-[85vh] overflow-hidden flex flex-col"
                    >
                        <DialogHeader>
                            <DialogTitle>{t('modelSelectorTitle')}</DialogTitle>
                            <DialogDescription>{t('modelSelectorDescription')}</DialogDescription>
                        </DialogHeader>

                        <div className="relative">
                            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                            <Input
                                value={fetchedModelsSearch}
                                onChange={(e) => setFetchedModelsSearch(e.target.value)}
                                placeholder={t('modelSelectorSearchPlaceholder')}
                                className="pl-9"
                            />
                        </div>

                        <div className="rounded-xl border border-border bg-muted/20 p-2 overflow-y-auto flex-1 min-h-48">
                            {filteredFetchedModels.length > 0 ? (
                                <div className="space-y-1">
                                    {filteredFetchedModels.map((model) => {
                                        const isSelected = selectedFetchedModels.includes(model);
                                        const isAdded = autoModels.includes(model);
                                        return (
                                            <button
                                                key={model}
                                                type="button"
                                                onClick={() => toggleFetchedModelSelection(model)}
                                                className={`w-full rounded-lg border px-3 py-2 text-left text-sm transition-colors flex items-center justify-between gap-3 ${
                                                    isSelected
                                                        ? 'border-primary bg-primary/10'
                                                        : 'border-transparent hover:border-border hover:bg-accent'
                                                }`}
                                            >
                                                <span className="truncate">{model}</span>
                                                <span className="shrink-0 flex items-center gap-1.5">
                                                    {isAdded && (
                                                        <Badge variant="secondary" className="text-[10px]">{t('modelSelectorAlreadyAdded')}</Badge>
                                                    )}
                                                    {isSelected && <Check className="h-4 w-4 text-primary" />}
                                                </span>
                                            </button>
                                        );
                                    })}
                                </div>
                            ) : (
                                <div className="h-full min-h-32 flex items-center justify-center text-sm text-muted-foreground">
                                    {t('modelSelectorEmpty')}
                                </div>
                            )}
                        </div>

                        <div className="flex items-center justify-end gap-2 pt-2 border-t">
                            <Button
                                type="button"
                                variant="secondary"
                                onClick={() => setShowFetchedModelsDialog(false)}
                            >
                                {t('modelSelectorCancel')}
                            </Button>
                            <Button
                                type="button"
                                onClick={handleConfirmFetchedModels}
                                disabled={selectedFetchedModels.length === 0}
                            >
                                {modelSelectorConfirmLabel}
                            </Button>
                        </div>
                    </DialogContent>
                </Dialog>

                <div className="relative">
                    <Input
                        ref={inputRef}
                        id={`${idPrefix}-model-custom`}
                        type="text"
                        value={inputValue}
                        onChange={(e) => setInputValue(e.target.value)}
                        onKeyDown={handleInputKeyDown}
                        placeholder={t('modelCustomPlaceholder')}
                        className="pr-10 rounded-xl"
                    />
                    {inputValue.trim() && !customModels.includes(inputValue.trim()) && !autoModels.includes(inputValue.trim()) && (
                        <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            onClick={() => handleAddModel(inputValue)}
                            className="absolute rounded-lg right-1 top-1/2 -translate-y-1/2 h-7 w-7 p-0 text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
                            title={t('modelAdd')}
                        >
                            <Plus className="size-4" />
                        </Button>
                    )}
                </div>

                <div className="space-y-2">
                    <div className="flex items-center justify-between">
                        <label className="text-xs font-medium text-card-foreground">
                            {t('modelSelected')} {(autoModels.length + customModels.length) > 0 && `(${autoModels.length + customModels.length})`}
                        </label>
                        {(autoModels.length + customModels.length) > 0 && (
                            <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                onClick={() => {
                                    updateModels([], []);
                                }}
                                className="h-6 px-2 text-xs text-muted-foreground/50 hover:text-muted-foreground hover:bg-transparent"
                            >
                                {t('modelClearAll')}
                            </Button>
                        )}
                    </div>
                    <div className="rounded-xl border border-border bg-muted/30 p-2.5 max-h-40 min-h-12 overflow-y-auto">
                        {(autoModels.length + customModels.length) > 0 ? (
                            <div className="flex flex-wrap gap-1.5">
                                {autoModels.map((model) => (
                                    <Badge key={model} variant="secondary" className="bg-muted hover:bg-muted/80">
                                        {model}
                                        <button
                                            type="button"
                                            onClick={() => handleRemoveAutoModel(model)}
                                            className="ml-1 rounded-sm opacity-70 hover:opacity-100 focus:outline-none focus:ring-1 focus:ring-ring"
                                        >
                                            <X className="h-3 w-3" />
                                        </button>
                                    </Badge>
                                ))}
                                {customModels.map((model) => (
                                    <Badge key={model} className="bg-primary hover:bg-primary/90">
                                        {model}
                                        <button
                                            type="button"
                                            onClick={() => handleRemoveCustomModel(model)}
                                            className="ml-1 rounded-sm opacity-70 hover:opacity-100 focus:outline-none focus:ring-1 focus:ring-ring"
                                        >
                                            <X className="h-3 w-3" />
                                        </button>
                                    </Badge>
                                ))}
                            </div>
                        ) : (
                            <div className="flex items-center justify-center h-8 text-xs text-muted-foreground">
                                {t('modelNoSelected')}
                            </div>
                        )}
                    </div>
                </div>
            </div>

            <Accordion type="single" collapsible className="w-full border rounded-xl bg-card">
                <AccordionItem value="advanced" className="border-none">
                    <AccordionTrigger className="text-sm font-medium text-card-foreground py-3 px-4 hover:no-underline hover:bg-muted/30 rounded-xl transition-colors">
                        {t('advanced')}
                    </AccordionTrigger>
                    <AccordionContent className="pt-4 px-4 pb-4 space-y-4 border-t">
                        <TooltipProvider>
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            <div className="space-y-2">
                                <label htmlFor={`${idPrefix}-auto-group`} className="text-sm font-medium text-card-foreground">
                                    {t('autoGroup')}
                                </label>
                                <Select
                                    value={String(formData.auto_group)}
                                    onValueChange={(value) => onFormDataChange({ ...formData, auto_group: Number(value) as AutoGroupType })}
                                >
                                    <SelectTrigger id={`${idPrefix}-auto-group`} className="rounded-xl w-full border border-border px-4 py-2 text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                                        <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent className='rounded-xl'>
                                        <SelectItem className='rounded-xl' value={String(AutoGroupType.None)}>{t('autoGroupNone')}</SelectItem>
                                        <SelectItem className='rounded-xl' value={String(AutoGroupType.Fuzzy)}>{t('autoGroupFuzzy')}</SelectItem>
                                        <SelectItem className='rounded-xl' value={String(AutoGroupType.Exact)}>{t('autoGroupExact')}</SelectItem>
                                        <SelectItem className='rounded-xl' value={String(AutoGroupType.Regex)}>{t('autoGroupRegex')}</SelectItem>
                                    </SelectContent>
                                </Select>
                            </div>

                            <div className="space-y-2">
                                <label htmlFor={`${idPrefix}-channel-proxy`} className="text-sm font-medium text-card-foreground">
                                    {t('channelProxy')}
                                </label>
                                <Input
                                    id={`${idPrefix}-channel-proxy`}
                                    type="text"
                                    value={formData.channel_proxy}
                                    onChange={(e) => onFormDataChange({ ...formData, channel_proxy: e.target.value })}
                                    placeholder={t('channelProxyPlaceholder')}
                                    className="rounded-xl"
                                />
                            </div>
                        </div>

                        <div className="space-y-2">
                            <div className="flex items-center justify-between">
                                <label className="text-sm font-medium text-card-foreground">
                                    {t('customHeader')} {formData.custom_header.length > 0 ? `(${formData.custom_header.length})` : ''}
                                </label>
                                <div className="flex items-center gap-2">
                                    <Tooltip>
                                        <TooltipTrigger asChild>
                                            <button
                                                type="button"
                                                className="inline-flex h-6 w-6 items-center justify-center rounded-full text-muted-foreground hover:text-foreground"
                                                aria-label={t('customHeaderHelpTitle')}
                                            >
                                                <HelpCircle className="h-4 w-4" />
                                            </button>
                                        </TooltipTrigger>
                                        <TooltipContent className="max-w-xs whitespace-pre-line">
                                            {`${t('customHeaderHelpTitle')}\n${t('customHeaderHelpDescription')}\n${t('customHeaderHelpExampleAdd')}\n${t('customHeaderHelpExampleDelete')}`}
                                        </TooltipContent>
                                    </Tooltip>
                                    <Button
                                        type="button"
                                        variant="ghost"
                                        size="sm"
                                        onClick={handleAddHeader}
                                        className="h-6 px-2 text-xs text-muted-foreground/70 hover:text-muted-foreground hover:bg-transparent"
                                    >
                                        <Plus className="h-3 w-3 mr-1" />
                                        {t('customHeaderAdd')}
                                    </Button>
                                </div>
                            </div>
                            <div className="space-y-2">
                                {(formData.custom_header ?? []).map((h, idx) => (
                                    <div key={`hdr-${idx}`} className="flex items-center gap-2">
                                        <Input
                                            type="text"
                                            value={h.header_key}
                                            onChange={(e) => handleUpdateHeader(idx, { header_key: e.target.value })}
                                            placeholder={t('customHeaderKey')}
                                            className="rounded-xl flex-1"
                                        />
                                        <Input
                                            type="text"
                                            value={h.header_value}
                                            onChange={(e) => handleUpdateHeader(idx, { header_value: e.target.value })}
                                            placeholder={t('customHeaderValue')}
                                            className="rounded-xl flex-1"
                                        />
                                        <Button
                                            type="button"
                                            variant="ghost"
                                            size="sm"
                                            onClick={() => handleRemoveHeader(idx)}
                                            disabled={(formData.custom_header ?? []).length <= 1}
                                            className="h-8 w-8 p-0 rounded-xl text-muted-foreground hover:text-destructive hover:bg-transparent disabled:opacity-40"
                                            title="Remove"
                                        >
                                            <X className="h-4 w-4" />
                                        </Button>
                                    </div>
                                ))}
                            </div>
                        </div>

                        <div className="space-y-2">
                            <label htmlFor={`${idPrefix}-match-regex`} className="text-sm font-medium text-card-foreground">
                                {t('matchRegex')}
                            </label>
                            <Input
                                id={`${idPrefix}-match-regex`}
                                type="text"
                                value={formData.match_regex}
                                onChange={(e) => onFormDataChange({ ...formData, match_regex: e.target.value })}
                                placeholder={t('matchRegexPlaceholder')}
                                className="rounded-xl"
                            />
                        </div>

                        <div className="space-y-2">
                            <div className="flex items-center gap-2">
                                <label htmlFor={`${idPrefix}-param-override`} className="text-sm font-medium text-card-foreground">
                                    {t('paramOverride')}
                                </label>
                                <Tooltip>
                                    <TooltipTrigger asChild>
                                        <button
                                            type="button"
                                            className="inline-flex h-6 w-6 items-center justify-center rounded-full text-muted-foreground hover:text-foreground"
                                            aria-label={t('paramOverrideHelpTitle')}
                                        >
                                            <HelpCircle className="h-4 w-4" />
                                        </button>
                                    </TooltipTrigger>
                                    <TooltipContent className="max-w-xs whitespace-pre-line">
                                        {`${t('paramOverrideHelpTitle')}\n${t('paramOverrideHelpDescription')}\n${t('paramOverrideHelpExampleUpdate')}\n${t('paramOverrideHelpExampleDelete')}`}
                                    </TooltipContent>
                                </Tooltip>
                            </div>
                            <textarea
                                id={`${idPrefix}-param-override`}
                                value={formData.param_override}
                                onChange={(e) => onFormDataChange({ ...formData, param_override: e.target.value })}
                                placeholder={t('paramOverridePlaceholder')}
                                className="min-h-28 w-full rounded-xl border border-border bg-background px-3 py-2 text-sm text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                            />
                        </div>
                        </TooltipProvider>
                    </AccordionContent>
                </AccordionItem>
            </Accordion>

            <div className="flex flex-wrap items-center justify-between gap-4 p-4 rounded-xl bg-muted/20 border border-border/50">
                <label className="flex items-center gap-2 cursor-pointer">
                    <Switch
                        checked={formData.enabled}
                        onCheckedChange={(checked) => onFormDataChange({ ...formData, enabled: checked })}
                    />
                    <span className="text-sm font-medium text-card-foreground">{t('enabled')}</span>
                </label>
                <div className="flex items-center gap-6">
                    <label className="flex items-center gap-2 cursor-pointer">
                        <Switch
                            checked={formData.proxy}
                            onCheckedChange={(checked) => onFormDataChange({ ...formData, proxy: checked })}
                        />
                        <span className="text-sm text-card-foreground">{t('proxy')}</span>
                    </label>
                    <label className="flex items-center gap-2 cursor-pointer">
                        <Switch
                            checked={formData.auto_sync}
                            onCheckedChange={(checked) => onFormDataChange({ ...formData, auto_sync: checked })}
                        />
                        <span className="text-sm text-card-foreground">{t('autoSync')}</span>
                    </label>
                </div>
            </div>

            <div className={`flex flex-col gap-3 pt-2 ${onCancel ? 'sm:flex-row' : ''}`}>
                {onCancel && cancelText && (
                    <Button
                        type="button"
                        variant="secondary"
                        onClick={onCancel}
                        className="w-full sm:flex-1 rounded-2xl h-12"
                    >
                        {cancelText}
                    </Button>
                )}
                <Button
                    type="submit"
                    disabled={isPending}
                    className="w-full sm:flex-1 rounded-2xl h-12"
                >
                    {isPending ? pendingText : submitText}
                </Button>
            </div>
        </form>
    );
}
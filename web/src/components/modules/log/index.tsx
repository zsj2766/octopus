'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { type LogFilters, type RelayLogStatusFilter, useLogs } from '@/api/endpoints/log';
import { useChannelList } from '@/api/endpoints/channel';
import { useModelList } from '@/api/endpoints/model';
import { PageWrapper } from '@/components/common/PageWrapper';
import { LogCard } from './Item';
import { Loader2, Search, X, CalendarDays } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Input } from '@/components/ui/input';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { Button } from '@/components/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Calendar } from '@/components/ui/calendar';
import type { DateRange } from 'react-day-picker';

const FILTER_ALL_VALUE = '__all__';
const FILTER_TRUE_VALUE = 'true';
const FILTER_FALSE_VALUE = 'false';
const SEARCH_TRIGGER_WIDTH = 36;
const SEARCH_EXPANDED_WIDTH = 220;

type RetryFilterValue = typeof FILTER_ALL_VALUE | typeof FILTER_TRUE_VALUE | typeof FILTER_FALSE_VALUE;

type StatusFilterValue = typeof FILTER_ALL_VALUE | RelayLogStatusFilter;

function getRangeTimestamp(range?: DateRange): { start_time?: number; end_time?: number } {
    if (!range?.from && !range?.to) return {};

    if (range?.from) {
        const start = new Date(range.from);
        start.setHours(0, 0, 0, 0);

        if (range?.to) {
            const end = new Date(range.to);
            end.setHours(23, 59, 59, 999);
            return {
                start_time: Math.floor(start.getTime() / 1000),
                end_time: Math.floor(end.getTime() / 1000),
            };
        }

        return {
            start_time: Math.floor(start.getTime() / 1000),
        };
    }

    const end = new Date(range!.to!);
    end.setHours(23, 59, 59, 999);
    return {
        end_time: Math.floor(end.getTime() / 1000),
    };
}

/**
 * 日志页面组件
 * - 初始加载20条历史日志
 * - SSE 实时推送新日志
 * - 滚动自动加载更多
 */
export function Log() {
    const t = useTranslations('log');
    const { data: channelsData } = useChannelList();
    const { data: modelsData } = useModelList();

    const [status, setStatus] = useState<StatusFilterValue>(FILTER_ALL_VALUE);
    const [channelID, setChannelID] = useState<string>(FILTER_ALL_VALUE);
    const [model, setModel] = useState<string>(FILTER_ALL_VALUE);
    const [retry, setRetry] = useState<RetryFilterValue>(FILTER_ALL_VALUE);
    const [keywordInput, setKeywordInput] = useState('');
    const [keyword, setKeyword] = useState('');
    const [searchExpanded, setSearchExpanded] = useState(false);
    const [dateRange, setDateRange] = useState<DateRange | undefined>();

    const channelOptions = useMemo(() => {
        if (!channelsData) return [];
        return [...channelsData]
            .map((item) => ({ id: item.raw.id, name: item.raw.name }))
            .sort((a, b) => a.id - b.id);
    }, [channelsData]);

    const modelOptions = useMemo(() => {
        if (!modelsData) return [];
        return [...modelsData]
            .map((item) => item.name)
            .sort((a, b) => a.localeCompare(b));
    }, [modelsData]);

    const filters = useMemo<LogFilters>(() => {
        const next: LogFilters = {};
        const rangeFilter = getRangeTimestamp(dateRange);
        if (typeof rangeFilter.start_time === 'number') next.start_time = rangeFilter.start_time;
        if (typeof rangeFilter.end_time === 'number') next.end_time = rangeFilter.end_time;

        if (status !== FILTER_ALL_VALUE) next.status = status;
        if (channelID !== FILTER_ALL_VALUE) next.channel_id = Number(channelID);
        if (model !== FILTER_ALL_VALUE) next.model = model;
        if (keyword.trim()) next.keyword = keyword.trim();
        if (retry === FILTER_TRUE_VALUE) next.has_retry = true;
        if (retry === FILTER_FALSE_VALUE) next.has_retry = false;

        return next;
    }, [channelID, dateRange, keyword, model, retry, status]);

    const { logs, hasMore, isLoading, isLoadingMore, loadMore } = useLogs({ pageSize: 10, filters });
    const loadMoreRef = useRef<HTMLDivElement>(null);
    const armedRef = useRef(true);

    useEffect(() => {
        const target = loadMoreRef.current;
        if (!target) return;

        const observer = new IntersectionObserver(
            (entries) => {
                const entry = entries[0];
                if (!entry) return;

                if (!entry.isIntersecting) {
                    armedRef.current = true;
                    return;
                }

                if (!armedRef.current) return;
                if (!hasMore || isLoading || isLoadingMore || logs.length === 0) return;

                armedRef.current = false;
                loadMore();
            },
            { rootMargin: '100px' }
        );

        observer.observe(target);
        return () => observer.disconnect();
    }, [hasMore, isLoading, isLoadingMore, loadMore, logs.length]);

    const applyKeyword = useCallback(() => {
        setKeyword(keywordInput.trim());
    }, [keywordInput]);

    const clearAllFilters = useCallback(() => {
        setStatus(FILTER_ALL_VALUE);
        setChannelID(FILTER_ALL_VALUE);
        setModel(FILTER_ALL_VALUE);
        setRetry(FILTER_ALL_VALUE);
        setKeyword('');
        setKeywordInput('');
        setDateRange(undefined);
        setSearchExpanded(false);
    }, []);

    const hasActiveFilter =
        status !== FILTER_ALL_VALUE ||
        channelID !== FILTER_ALL_VALUE ||
        model !== FILTER_ALL_VALUE ||
        retry !== FILTER_ALL_VALUE ||
        !!keyword ||
        !!dateRange?.from ||
        !!dateRange?.to;

    const dateLabel = useMemo(() => {
        if (!dateRange?.from) return t('filter.time.all');
        const from = dateRange.from.toLocaleDateString();
        if (!dateRange.to) return from;
        return `${from} ~ ${dateRange.to.toLocaleDateString()}`;
    }, [dateRange?.from, dateRange?.to, t]);

    return (
        <PageWrapper className="grid grid-cols-1 gap-4">
            <div className="rounded-3xl border bg-card custom-shadow p-3 flex flex-wrap items-center gap-2">
                <Select value={status} onValueChange={(value) => setStatus(value as StatusFilterValue)}>
                    <SelectTrigger className="h-9 w-36 rounded-xl border text-sm">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="rounded-xl">
                        <SelectItem value={FILTER_ALL_VALUE} className="rounded-lg">{t('filter.status.all')}</SelectItem>
                        <SelectItem value="success" className="rounded-lg">{t('filter.status.success')}</SelectItem>
                        <SelectItem value="failed" className="rounded-lg">{t('filter.status.failed')}</SelectItem>
                    </SelectContent>
                </Select>

                <Select value={channelID} onValueChange={setChannelID}>
                    <SelectTrigger className="h-9 w-44 rounded-xl border text-sm">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="rounded-xl">
                        <SelectItem value={FILTER_ALL_VALUE} className="rounded-lg">{t('filter.channel.all')}</SelectItem>
                        {channelOptions.map((channel) => (
                            <SelectItem key={channel.id} value={String(channel.id)} className="rounded-lg">
                                {channel.name}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>

                <Select value={model} onValueChange={setModel}>
                    <SelectTrigger className="h-9 w-48 rounded-xl border text-sm">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="rounded-xl">
                        <SelectItem value={FILTER_ALL_VALUE} className="rounded-lg">{t('filter.model.all')}</SelectItem>
                        {modelOptions.map((modelName) => (
                            <SelectItem key={modelName} value={modelName} className="rounded-lg">
                                {modelName}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>

                <Select value={retry} onValueChange={(value) => setRetry(value as RetryFilterValue)}>
                    <SelectTrigger className="h-9 w-36 rounded-xl border text-sm">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="rounded-xl">
                        <SelectItem value={FILTER_ALL_VALUE} className="rounded-lg">{t('filter.retry.all')}</SelectItem>
                        <SelectItem value={FILTER_TRUE_VALUE} className="rounded-lg">{t('filter.retry.yes')}</SelectItem>
                        <SelectItem value={FILTER_FALSE_VALUE} className="rounded-lg">{t('filter.retry.no')}</SelectItem>
                    </SelectContent>
                </Select>

                <Popover>
                    <PopoverTrigger asChild>
                        <Button
                            variant="outline"
                            className="h-9 rounded-xl px-3 text-sm font-normal min-w-[220px] justify-between"
                        >
                            <span className="truncate text-left">{dateLabel}</span>
                            <CalendarDays className="size-4 text-muted-foreground" />
                        </Button>
                    </PopoverTrigger>
                    <PopoverContent align="start" side="bottom" sideOffset={8} className="w-fit rounded-2xl border border-border/60 shadow-xl overflow-hidden bg-card p-0">
                        <Calendar
                            mode="range"
                            selected={dateRange}
                            onSelect={setDateRange}
                            numberOfMonths={2}
                            classNames={{ today: '' }}
                        />
                    </PopoverContent>
                </Popover>

                <div
                    className="relative h-9"
                    style={{ width: searchExpanded ? SEARCH_EXPANDED_WIDTH : SEARCH_TRIGGER_WIDTH }}
                >
                    {!searchExpanded ? (
                        <Button
                            variant="ghost"
                            size="icon"
                            className="absolute inset-0 rounded-xl text-muted-foreground hover:text-foreground"
                            onClick={() => setSearchExpanded(true)}
                            aria-label={t('filter.keyword.open')}
                        >
                            <Search className="size-4" />
                        </Button>
                    ) : (
                        <div className="absolute right-0 top-0 flex items-center gap-2 h-9 w-full px-3 rounded-xl border bg-background">
                            <Search className="size-4 text-muted-foreground shrink-0" />
                            <Input
                                value={keywordInput}
                                onChange={(e) => setKeywordInput(e.target.value)}
                                onKeyDown={(e) => {
                                    if (e.key === 'Enter') applyKeyword();
                                }}
                                autoFocus
                                className="h-full border-0 bg-transparent px-0 shadow-none focus-visible:ring-0"
                                placeholder={t('filter.keyword.placeholder')}
                            />
                            <Button
                                variant="ghost"
                                size="icon"
                                className="size-6 rounded-md text-muted-foreground hover:text-foreground"
                                onClick={() => {
                                    setKeywordInput('');
                                    setKeyword('');
                                    setSearchExpanded(false);
                                }}
                                aria-label={t('filter.keyword.clear')}
                            >
                                <X className="size-3.5" />
                            </Button>
                        </div>
                    )}
                </div>

                {searchExpanded && (
                    <Button variant="outline" className="h-9 rounded-xl px-3" onClick={applyKeyword}>
                        {t('filter.keyword.apply')}
                    </Button>
                )}

                {hasActiveFilter && (
                    <Button variant="ghost" className="h-9 rounded-xl px-3 text-muted-foreground" onClick={clearAllFilters}>
                        {t('filter.reset')}
                    </Button>
                )}
            </div>

            {logs.map((log) => (
                <LogCard key={`log-${log.id}`} log={log} />
            ))}

            <div ref={loadMoreRef} className="flex justify-center py-4">
                {hasMore && (isLoadingMore || isLoading) && (
                    <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                )}
                {!hasMore && logs.length > 0 && (
                    <span className="text-sm text-muted-foreground">{t('list.noMore')}</span>
                )}
            </div>
        </PageWrapper>
    );
}

import type { InfiniteData } from '@tanstack/react-query';
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient, API_BASE_URL } from '../client';
import { logger } from '@/lib/logger';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

/**
 * 尝试状态
 */
export type AttemptStatus = 'success' | 'failed' | 'circuit_break' | 'skipped';
export type DiffOperation = 'add' | 'replace' | 'remove';

export interface RequestDiffItem {
    path: string;
    operation: DiffOperation;
    before?: unknown;
    after?: unknown;
}

export interface HeaderDiffItem {
    header_key: string;
    operation: DiffOperation;
    before?: string[];
    after?: string[];
}

/**
 * 单次渠道尝试信息
 */
export interface ChannelAttempt {
    channel_id: number;
    channel_key_id?: number;
    channel_name: string;
    model_name: string;
    attempt_num: number;    // 第几次尝试
    status: AttemptStatus;
    duration: number;       // 耗时(毫秒)
    sticky?: boolean;
    msg?: string;
    request_diff?: RequestDiffItem[];
    header_diff?: HeaderDiffItem[];
}

/**
 * 日志数据
 */
export interface RelayLog {
    id: number;
    time: number;                // 时间戳
    request_model_name: string;  // 请求模型名称
    channel: number;             // 实际使用的渠道ID
    channel_name: string;        // 渠道名称
    actual_model_name: string;   // 实际使用模型名称
    input_tokens: number;        // 输入Token
    output_tokens: number;       // 输出Token
    ftut: number;                // 首字时间(毫秒)
    use_time: number;            // 总用时(毫秒)
    cost: number;                // 消耗费用
    request_content: string;     // 请求内容
    response_content: string;    // 响应内容
    error: string;               // 错误信息
    attempts?: ChannelAttempt[]; // 所有尝试记录
    total_attempts?: number;     // 总尝试次数
}

/**
 * 日志列表查询参数
 */
export type RelayLogStatusFilter = 'success' | 'failed';

export interface LogFilters {
    start_time?: number;
    end_time?: number;
    status?: RelayLogStatusFilter;
    channel_id?: number;
    model?: string;
    keyword?: string;
    has_retry?: boolean;
}


/**
 * 清空日志 Hook
 * 
 * @example
 * const clearLogs = useClearLogs();
 * 
 * clearLogs.mutate();
 */
export function useClearLogs() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async () => {
            return apiClient.delete<null>('/api/v1/log/clear');
        },
        onSuccess: () => {
            logger.log('日志清空成功');
            queryClient.invalidateQueries({ queryKey: ['logs'] });
        },
        onError: (error) => {
            logger.error('日志清空失败:', error);
        },
    });
}

type NormalizedLogFilters = Partial<LogFilters>;

function normalizeLogFilters(filters: LogFilters = {}): NormalizedLogFilters {
    const normalized: NormalizedLogFilters = {};

    if (typeof filters.start_time === 'number') {
        normalized.start_time = filters.start_time;
    }
    if (typeof filters.end_time === 'number') {
        normalized.end_time = filters.end_time;
    }
    if (filters.status) {
        normalized.status = filters.status;
    }
    if (typeof filters.channel_id === 'number') {
        normalized.channel_id = filters.channel_id;
    }
    if (filters.model?.trim()) {
        normalized.model = filters.model.trim();
    }
    if (filters.keyword?.trim()) {
        normalized.keyword = filters.keyword.trim();
    }
    if (typeof filters.has_retry === 'boolean') {
        normalized.has_retry = filters.has_retry;
    }

    return normalized;
}

function matchRelayLogFilters(log: RelayLog, filters: NormalizedLogFilters): boolean {
    if (typeof filters.start_time === 'number' && log.time < filters.start_time) {
        return false;
    }
    if (typeof filters.end_time === 'number' && log.time > filters.end_time) {
        return false;
    }
    if (filters.status === 'success' && log.error) {
        return false;
    }
    if (filters.status === 'failed' && !log.error) {
        return false;
    }
    if (typeof filters.channel_id === 'number' && log.channel !== filters.channel_id) {
        return false;
    }
    if (filters.model && log.actual_model_name !== filters.model) {
        return false;
    }

    const attempts = log.total_attempts ?? log.attempts?.length ?? 0;
    if (typeof filters.has_retry === 'boolean' && (attempts > 1) !== filters.has_retry) {
        return false;
    }

    if (filters.keyword) {
        const keyword = filters.keyword.toLowerCase();
        const requestContent = (log.request_content ?? '').toLowerCase();
        const responseContent = (log.response_content ?? '').toLowerCase();
        const errorContent = (log.error ?? '').toLowerCase();
        if (!requestContent.includes(keyword) && !responseContent.includes(keyword) && !errorContent.includes(keyword)) {
            return false;
        }
    }

    return true;
}

const logsInfiniteQueryKey = (pageSize: number, filters: NormalizedLogFilters) => ['logs', 'infinite', pageSize, filters] as const;

function buildLogListQueryParams(
    page: number,
    pageSize: number,
    filters: NormalizedLogFilters,
): Record<string, string | number | boolean> {
    const params: Record<string, string | number | boolean> = {
        page,
        page_size: pageSize,
    };

    if (typeof filters.start_time === 'number') params.start_time = filters.start_time;
    if (typeof filters.end_time === 'number') params.end_time = filters.end_time;
    if (filters.status) params.status = filters.status;
    if (typeof filters.channel_id === 'number') params.channel_id = filters.channel_id;
    if (filters.model) params.model = filters.model;
    if (filters.keyword) params.keyword = filters.keyword;
    if (typeof filters.has_retry === 'boolean') params.has_retry = filters.has_retry;

    return params;
}

/**
 * 日志管理 Hook
 * 整合初始加载、SSE 实时推送、滚动加载更多
 *
 * @example
 * const { logs, isConnected, hasMore, isLoadingMore, loadMore, clear } = useLogs();
 *
 * // logs 自动包含历史日志和实时日志，按时间倒序
 * logs.forEach(log => console.log(log.request_model_name));
 *
 * // 滚动到底部时加载更多
 * if (hasMore && !isLoadingMore) loadMore();
 */
export function useLogs(options: { pageSize?: number; filters?: LogFilters } = {}) {
    const { pageSize = 20, filters = {} } = options;

    const normalizedFilters = useMemo(
        () => normalizeLogFilters(filters),
        [
            filters.start_time,
            filters.end_time,
            filters.status,
            filters.channel_id,
            filters.model,
            filters.keyword,
            filters.has_retry,
        ],
    );

    const logsQueryKey = useMemo(
        () => logsInfiniteQueryKey(pageSize, normalizedFilters),
        [pageSize, normalizedFilters],
    );

    const [isConnected, setIsConnected] = useState(false);
    const [error, setError] = useState<Error | null>(null);
    const eventSourceRef = useRef<EventSource | null>(null);

    const queryClient = useQueryClient();

    const logsQuery = useInfiniteQuery({
        queryKey: logsQueryKey,
        initialPageParam: 1,
        queryFn: async ({ pageParam }) => {
            const result = await apiClient.get<RelayLog[] | null>(
                '/api/v1/log/list',
                buildLogListQueryParams(pageParam, pageSize, normalizedFilters),
            );
            return result ?? [];
        },
        getNextPageParam: (lastPage, allPages) => {
            if (!lastPage || lastPage.length < pageSize) return undefined;
            return allPages.length + 1;
        },
        staleTime: Infinity,
        refetchOnMount: 'always',
    });

    const logs = useMemo(() => {
        const pages = logsQuery.data?.pages ?? [];
        const seen = new Set<number>();
        const merged: RelayLog[] = [];

        for (const page of pages) {
            for (const log of page) {
                if (seen.has(log.id)) continue;
                seen.add(log.id);
                merged.push(log);
            }
        }

        merged.sort((a, b) => b.time - a.time);
        return merged;
    }, [logsQuery.data]);

    const loadMore = useCallback(async () => {
        if (!logsQuery.hasNextPage) return;
        if (logsQuery.isFetchingNextPage) return;

        try {
            await logsQuery.fetchNextPage();
        } catch (e) {
            logger.error('加载更多日志失败:', e);
        }
    }, [logsQuery]);

    useEffect(() => {
        let cancelled = false;

        const connect = async () => {
            try {
                const { token } = await apiClient.get<{ token: string }>('/api/v1/log/stream-token');
                if (cancelled) return;

                const eventSource = new EventSource(`${API_BASE_URL}/api/v1/log/stream?token=${token}`);
                eventSourceRef.current = eventSource;

                eventSource.onopen = () => {
                    setIsConnected(true);
                    setError(null);
                };

                eventSource.onmessage = (event) => {
                    try {
                        const log: RelayLog = JSON.parse(event.data);
                        if (!matchRelayLogFilters(log, normalizedFilters)) return;

                        queryClient.setQueryData(
                            logsQueryKey,
                            (old: InfiniteData<RelayLog[], number> | undefined) => {
                                if (!old) {
                                    return { pages: [[log]], pageParams: [1] };
                                }

                                const exists = old.pages.some((p) => p?.some((x) => x.id === log.id));
                                if (exists) return old;

                                const firstPage = old.pages[0] ?? [];
                                return { ...old, pages: [[log, ...firstPage], ...old.pages.slice(1)] };
                            },
                        );
                    } catch (e) {
                        logger.error('解析日志数据失败:', e);
                    }
                };

                eventSource.onerror = () => {
                    setIsConnected(false);
                    setError(new Error('SSE 连接断开'));
                    eventSource.close();
                    eventSourceRef.current = null;
                };
            } catch (e) {
                if (cancelled) return;
                setError(e instanceof Error ? e : new Error('获取 stream token 失败'));
                logger.error('获取 stream token 失败:', e);
            }
        };

        connect();

        return () => {
            cancelled = true;
            eventSourceRef.current?.close();
            eventSourceRef.current = null;
            setIsConnected(false);
        };
    }, [queryClient, logsQueryKey, normalizedFilters]);

    const clear = useCallback(() => {
        queryClient.removeQueries({ queryKey: logsQueryKey });
    }, [logsQueryKey, queryClient]);

    return {
        logs,
        isConnected,
        error,
        hasMore: !!logsQuery.hasNextPage,
        isLoading: logsQuery.isLoading,
        isLoadingMore: logsQuery.isFetchingNextPage,
        loadMore,
        clear,
    };
}

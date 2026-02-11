import { create } from 'zustand';
import type { NavItem } from '@/components/modules/navbar';

interface SearchState {
    searchTerms: Partial<Record<NavItem, string>>;
    channelTypeFilters: Partial<Record<NavItem, string>>;
    getSearchTerm: (page: NavItem) => string;
    setSearchTerm: (page: NavItem, term: string) => void;
    getChannelTypeFilter: (page: NavItem) => string;
    setChannelTypeFilter: (page: NavItem, type: string) => void;
}

export const useSearchStore = create<SearchState>((set, get) => ({
    searchTerms: {},
    channelTypeFilters: {},
    getSearchTerm: (page) => get().searchTerms[page] || '',
    setSearchTerm: (page, term) => set((state) => ({
        searchTerms: { ...state.searchTerms, [page]: term }
    })),
    getChannelTypeFilter: (page) => get().channelTypeFilters[page] || 'all',
    setChannelTypeFilter: (page, type) => set((state) => ({
        channelTypeFilters: { ...state.channelTypeFilters, [page]: type }
    })),
}));

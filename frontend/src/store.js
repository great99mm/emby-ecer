import { create } from 'zustand';
import { api } from './api';

const useStore = create((set, get) => ({
  token: localStorage.getItem('auth_token') || '',
  username: '',
  settings: {},
  scan: null,
  missing: [],
  seriesSearches: {},
  transfers: {},
  activeJobId: null,
  jobStatus: null,
  expandedSeries: {},

  setToken: (token) => {
    localStorage.setItem('auth_token', token);
    set({ token });
  },
  logout: () => {
    localStorage.removeItem('auth_token');
    set({ token: '', username: '', scan: null, missing: [], seriesSearches: {}, transfers: {}, activeJobId: null, jobStatus: null, expandedSeries: {} });
  },
  setSettings: (settings) => set({ settings }),
  setScan: (scan) => set({ scan, missing: scan?.missing || [] }),
  setSeriesSearch: (seriesKey, updater) => set(state => ({
    seriesSearches: { ...state.seriesSearches, [seriesKey]: typeof updater === 'function' ? updater(state.seriesSearches[seriesKey]) : updater }
  })),
  setTransfer: (key, data) => set(state => ({
    transfers: { ...state.transfers, [key]: data }
  })),
  setActiveJobId: (id) => set({ activeJobId: id }),
  clearJob: () => {
    set({ activeJobId: null, jobStatus: null });
  },
  setJobStatus: (jobStatus) => set({ jobStatus }),
  setExpandedSeries: (key, expanded) => set(state => ({
    expandedSeries: { ...state.expandedSeries, [key]: expanded }
  })),
  applySearchResults: (items) => {
    const updates = {};
    for (const item of items) {
      const ep = item.episodes?.[0] || {};
      const seriesKey = `series:${ep.tmdbId || item.title}`;
      const codes = (item.episodes || []).map(e => e.code).join('、');
      updates[seriesKey] = item.error
        ? { loading: false, error: item.error, codes }
        : { loading: false, results: item.results || [], query: item.title, codes };
    }
    set(state => ({ seriesSearches: { ...state.seriesSearches, ...updates } }));
  },

  // App initialization
  init: async () => {
    const token = get().token;
    if (!token) return;
    try {
      const me = await api('/api/auth/verify', { method: 'POST' });
      const settings = await api('/api/settings');
      set({ username: me.username, settings });
      try {
        const scan = await api('/api/scan/last');
        if (scan?.scannedAt) set({ scan, missing: scan.missing || [] });
      } catch {}
      try {
        const saved = await api('/api/search-results');
        if (Array.isArray(saved)) get().applySearchResults(saved);
      } catch {}
    } catch {
      localStorage.removeItem('auth_token');
      set({ token: '', username: '', scan: null, missing: [], seriesSearches: {}, transfers: {}, activeJobId: null, jobStatus: null });
    }
  },
}));

export default useStore;

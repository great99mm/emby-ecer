import { create } from 'zustand';
import { api } from './api';

export const isScanBusy = state => state.scanStarting || !!(state.jobStatus && !['done', 'error'].includes(state.jobStatus.status));

const useStore = create((set, get) => ({
  token: localStorage.getItem('auth_token') || '',
  username: '',
  settings: {},
  connectionStatus: {},
  scan: null,
  missing: [],
  seriesSearches: {},
  activeJobId: null,
  jobStatus: null,
  scanStarting: false,

  startScan: async (options = {}, message = '正在准备扫描…') => {
    if (isScanBusy(get())) return get().activeJobId;
    const previous = get().jobStatus;
    // Give immediate feedback and prevent repeated clicks before the POST returns.
    set({ scanStarting: true, jobStatus: { status: 'pending', progress: 0, message } });
    try {
      const data = await api('/api/jobs', { method: 'POST', body: JSON.stringify({ type: 'scan', airedOnly: true, ...options }) });
      set({ activeJobId: data.jobId, jobStatus: { id: data.jobId, status: 'pending', progress: 0, message } });
      return data.jobId;
    } catch (err) {
      set({ jobStatus: previous });
      throw err;
    } finally {
      set({ scanStarting: false });
    }
  },

  setToken: (token) => {
    localStorage.setItem('auth_token', token);
    set({ token, connectionStatus: {} });
  },
  logout: () => {
    localStorage.removeItem('auth_token');
    set({ token: '', username: '', settings: {}, connectionStatus: {}, scan: null, missing: [], seriesSearches: {}, activeJobId: null, jobStatus: null });
  },
  setSettings: (settings) => set({ settings, connectionStatus: {} }),
  checkConnections: async () => {
    const snapshot = get().settings;
    const token = get().token;
    if (!token || !snapshot.ready) return;
    await Promise.allSettled(['emby', 'tmdb', 'mp'].map(async target => {
      if (get().connectionStatus[target]?.status === 'checking') return;
      const pending = { status: 'checking' };
      set(state => ({ connectionStatus: { ...state.connectionStatus, [target]: pending } }));
      let result;
      try {
        const data = await api('/api/settings/test', { method: 'POST', body: JSON.stringify({ target }) });
        const item = data[target];
        result = {
          status: item?.configured === false ? 'unconfigured' : item?.ok === true ? 'connected' : 'failed',
          error: item?.error || (item?.ok ? '' : '未收到有效的检测结果'),
          checkedAt: item?.checkedAt || new Date().toISOString(),
          latencyMs: item?.latencyMs,
        };
      } catch (err) {
        result = { status: 'failed', error: err.message, checkedAt: new Date().toISOString() };
      }
      // Discard results belonging to an earlier configuration or login session.
      if (get().settings !== snapshot || get().token !== token || get().connectionStatus[target] !== pending) return;
      set(state => ({ connectionStatus: { ...state.connectionStatus, [target]: result } }));
    }));
  },
  setScan: (scan) => set({ scan, missing: scan?.missing || [] }),
  setSeriesSearch: (seriesKey, updater) => set(state => ({
    seriesSearches: { ...state.seriesSearches, [seriesKey]: typeof updater === 'function' ? updater(state.seriesSearches[seriesKey]) : updater }
  })),
  setActiveJobId: (id) => set({ activeJobId: id }),
  clearJob: () => {
    set({ activeJobId: null, jobStatus: null });
  },
  setJobStatus: (jobStatus) => set({ jobStatus }),
  // App initialization
  init: async () => {
    const token = get().token;
    if (!token) return;
    try {
      const me = await api('/api/auth/verify', { method: 'POST' });
      const settings = await api('/api/settings');
      set({ username: me.username, settings, connectionStatus: {} });
      try {
        const scan = await api('/api/scan/last');
        if (scan?.scannedAt) set({ scan, missing: scan.missing || [] });
      } catch {}
    } catch {
      localStorage.removeItem('auth_token');
      set({ token: '', username: '', settings: {}, connectionStatus: {}, scan: null, missing: [], seriesSearches: {}, activeJobId: null, jobStatus: null });
    }
  },
}));

export default useStore;

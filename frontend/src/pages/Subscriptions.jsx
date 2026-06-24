import { useEffect, useState } from 'react';
import { api } from '../api';
import toast from 'react-hot-toast';
import useStore from '../store';
import SearchResults from '../components/SearchResults';
import StatCard from '../components/StatCard';
import { CheckCircle2, Download, ExternalLink, Film, Play, Plus, Search, Sparkles, Trash2, RefreshCw, Timer, Tv, Unlock, X } from 'lucide-react';

const emptyForm = { title: '', mediaType: 'tv', tmdbId: '', season: '', enabled: true, autoTransfer: false, targetCid: '0' };
const TMDB_IMG = 'https://image.tmdb.org/t/p/w342';

export default function Subscriptions() {
  const missing = useStore(s => s.missing);
  const [items, setItems] = useState([]);
  const [running, setRunning] = useState(false);
  const [form, setForm] = useState(emptyForm);
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState(null);
  const [tmdbQuery, setTmdbQuery] = useState('');
  const [tmdbMediaType, setTmdbMediaType] = useState('tv');
  const [tmdbLoading, setTmdbLoading] = useState(false);
  const [tmdbResults, setTmdbResults] = useState([]);
  const [selectedTMDB, setSelectedTMDB] = useState(null);

  const enabledCount = items.filter(item => item.enabled).length;
  const lockedCount = items.reduce((sum, item) => sum + (item.lastResults || []).filter(r => r.source === 'HDHive' && r.hdhiveLocked).length, 0);
  const resultCount = items.reduce((sum, item) => sum + (item.lastResults || []).length, 0);
  const missingBySubscription = buildSubscriptionMissingMap(missing);

  const load = async () => {
    try {
      const data = await api('/api/subscriptions');
      setItems(data.items || []);
      setRunning(!!data.running);
    } catch (err) {
      toast.error(err.message);
    }
  };

  useEffect(() => { load(); }, []);

  const update = (name, value) => setForm(prev => ({ ...prev, [name]: value }));

  const searchTMDB = async (e) => {
    e.preventDefault();
    if (!tmdbQuery.trim()) return toast.error('请输入要搜索的标题');
    setTmdbLoading(true);
    setSelectedTMDB(null);
    try {
      const data = await api('/api/tmdb/search', {
        method: 'POST',
        body: JSON.stringify({ query: tmdbQuery.trim(), mediaType: tmdbMediaType }),
      });
      const results = data.results || [];
      setTmdbResults(results);
      if (results.length === 0) toast.error('TMDB 没有搜到匹配条目');
    } catch (err) {
      toast.error(err.message);
    } finally {
      setTmdbLoading(false);
    }
  };

  const selectTMDB = (item) => {
    setSelectedTMDB(item);
    setForm(prev => ({
      ...prev,
      title: item.title || '',
      mediaType: item.mediaType || tmdbMediaType,
      tmdbId: String(item.tmdbId || ''),
      season: item.mediaType === 'movie' ? '' : prev.season,
    }));
  };

  const save = async (e) => {
    e.preventDefault();
    if (!selectedTMDB) return toast.error('请先从 TMDB 搜索结果中选择一个条目');
    setLoading(true);
    try {
      await api('/api/subscriptions', {
        method: 'POST',
        body: JSON.stringify({
          ...form,
          title: selectedTMDB.title,
          mediaType: selectedTMDB.mediaType,
          tmdbId: Number(selectedTMDB.tmdbId || 0),
          tmdbYear: selectedTMDB.year || '',
          posterPath: selectedTMDB.posterPath || '',
          overview: selectedTMDB.overview || '',
          season: Number(form.season || 0),
        }),
      });
      setForm(emptyForm);
      setSelectedTMDB(null);
      setTmdbResults([]);
      setTmdbQuery('');
      await load();
      toast.success('订阅已保存');
    } catch (err) {
      toast.error(err.message);
    } finally {
      setLoading(false);
    }
  };

  const remove = async (id) => {
    if (!window.confirm('确认删除这个订阅？')) return;
    try {
      const data = await api('/api/subscriptions/delete', { method: 'POST', body: JSON.stringify({ ids: [id] }) });
      setItems(data.items || []);
      toast.success('已删除');
    } catch (err) {
      toast.error(err.message);
    }
  };

  const run = async (id = '') => {
    setRunning(true);
    setResult(null);
    try {
      const data = await api('/api/subscriptions/run', { method: 'POST', body: JSON.stringify({ ids: id ? [id] : [] }) });
      setResult(data);
      await load();
      toast.success('订阅扫描完成');
    } catch (err) {
      toast.error(err.message);
    } finally {
      setRunning(false);
    }
  };

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 xl:grid-cols-5 gap-3 sm:gap-4">
        <StatCard label="订阅" value={items.length} icon={Timer} />
        <StatCard label="启用中" value={enabledCount} icon={CheckCircle2} />
        <StatCard label="候选资源" value={resultCount} icon={Search} />
        <StatCard label="待审批" value={lockedCount} icon={Unlock} accent />
        <StatCard label="自动转存" value={items.filter(item => item.autoTransfer).length} icon={Download} />
      </div>

      <div className="card overflow-hidden p-0">
        <div className="bg-gradient-to-r from-primary-600 via-primary-500 to-blue-500 px-5 py-5 text-white">
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="text-xs font-bold uppercase tracking-[0.18em] text-primary-100">Subscription Scan</p>
              <h1 className="mt-1 text-2xl font-black tracking-tight">订阅与自动扫描</h1>
              <p className="mt-2 text-sm text-blue-100">按 TMDB 订阅追踪 PanSou、HDHive 和 MoviePilot 资源，点开卡片可搜索、解锁和转存。</p>
            </div>
            <div className="hidden sm:flex h-12 w-12 shrink-0 items-center justify-center rounded-lg bg-white/15 backdrop-blur">
              <Timer className="w-6 h-6" />
            </div>
          </div>
        </div>
        <div className="px-5 py-4">
          <button onClick={() => run()} disabled={running || !items.length} className="btn-primary w-full flex items-center justify-center gap-2 text-sm disabled:opacity-50">
            {running ? <RefreshCw className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />} 扫描全部
          </button>
        </div>
      </div>

      <div className="card space-y-4">
        <div>
          <div className="flex items-center gap-2 text-sm font-bold text-gray-900"><Plus className="h-4 w-4 text-primary-600" /> 新增订阅</div>
          <p className="mt-1 text-xs text-gray-400">先搜索 TMDB，确认海报和简介后再加入订阅，避免订错同名资源。</p>
        </div>

        <form onSubmit={searchTMDB} className="grid gap-2 md:grid-cols-[1fr_120px_120px]">
          <input className="rounded-md border border-gray-300 bg-gray-50 px-4 py-2.5 text-sm font-medium" placeholder="搜索片名，例如 凡人修仙传" value={tmdbQuery} onChange={e => setTmdbQuery(e.target.value)} />
          <select className="rounded-md border border-gray-300 bg-gray-50 px-3 py-2.5 text-sm font-medium" value={tmdbMediaType} onChange={e => setTmdbMediaType(e.target.value)}>
            <option value="tv">剧集</option>
            <option value="movie">电影</option>
          </select>
          <button disabled={tmdbLoading} className="btn-primary flex items-center justify-center gap-2 disabled:opacity-50">
            {tmdbLoading ? <RefreshCw className="h-4 w-4 animate-spin" /> : <Search className="h-4 w-4" />} 搜索
          </button>
        </form>

        {tmdbResults.length > 0 && (
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {tmdbResults.map(item => {
              const active = selectedTMDB?.tmdbId === item.tmdbId && selectedTMDB?.mediaType === item.mediaType;
              return (
                <button key={`${item.mediaType}-${item.tmdbId}`} type="button" onClick={() => selectTMDB(item)} className={`group flex gap-3 rounded-lg border p-2 text-left transition ${active ? 'border-primary-400 bg-primary-50 ring-2 ring-primary-100' : 'border-gray-200 bg-gray-50 hover:border-primary-200 hover:bg-white'}`}>
                  {item.posterPath ? <img src={TMDB_IMG + item.posterPath} className="h-24 w-16 rounded-md object-cover bg-gray-200" alt="" /> : <div className="flex h-24 w-16 shrink-0 items-center justify-center rounded-md bg-gray-200 text-xl text-gray-400">🎬</div>}
                  <div className="min-w-0 flex-1">
                    <div className="flex items-start gap-1">
                      <p className="line-clamp-2 text-sm font-bold text-gray-900">{item.title}</p>
                      {active && <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-primary-600" />}
                    </div>
                    <p className="mt-1 text-[11px] font-semibold text-gray-400">{item.mediaType === 'movie' ? '电影' : '剧集'} · {item.year || '未知年份'} · TMDB {item.tmdbId}</p>
                    <p className="mt-1 line-clamp-2 text-xs text-gray-500">{item.overview || '暂无简介，点开后可用 TMDB ID 确认。'}</p>
                  </div>
                </button>
              );
            })}
          </div>
        )}

        {selectedTMDB && (
          <form onSubmit={save} className="rounded-xl border border-primary-100 bg-gradient-to-br from-primary-50 to-white p-3">
            <div className="flex gap-3">
              {selectedTMDB.posterPath ? <img src={TMDB_IMG + selectedTMDB.posterPath} className="h-36 w-24 rounded-lg object-cover bg-gray-200 shadow-sm" alt="" /> : <div className="flex h-36 w-24 shrink-0 items-center justify-center rounded-lg bg-gray-200 text-2xl text-gray-400">🎬</div>}
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <h2 className="text-lg font-black text-gray-900">{selectedTMDB.title}</h2>
                  <span className="rounded-full bg-white px-2 py-0.5 text-[11px] font-bold text-primary-600">{selectedTMDB.mediaType === 'movie' ? '电影' : '剧集'}</span>
                  {selectedTMDB.year && <span className="text-xs font-semibold text-gray-400">{selectedTMDB.year}</span>}
                </div>
                {selectedTMDB.originalTitle && selectedTMDB.originalTitle !== selectedTMDB.title && <p className="mt-0.5 text-xs text-gray-400">原名：{selectedTMDB.originalTitle}</p>}
                <p className="mt-2 text-sm leading-6 text-gray-600">{selectedTMDB.overview || 'TMDB 暂无中文简介，请用海报、年份和 TMDB ID 确认是否为目标条目。'}</p>
                <a href={selectedTMDB.tmdbUrl} target="_blank" rel="noreferrer" className="mt-2 inline-flex items-center gap-1 text-xs font-bold text-primary-600 hover:text-primary-700">打开 TMDB <ExternalLink className="h-3.5 w-3.5" /></a>
              </div>
            </div>

            <div className="mt-3 grid grid-cols-2 gap-3">
              <input className="rounded-md border border-gray-300 bg-white px-4 py-2.5 text-sm font-medium disabled:bg-gray-100 disabled:text-gray-400" placeholder={selectedTMDB.mediaType === 'movie' ? '电影无需季号' : '季号，留空=全季'} value={form.season} disabled={selectedTMDB.mediaType === 'movie'} onChange={e => update('season', e.target.value)} />
              <input className="rounded-md border border-gray-300 bg-white px-4 py-2.5 text-sm font-medium" placeholder="目标 CID" value={form.targetCid} onChange={e => update('targetCid', e.target.value)} />
            </div>
            <div className="mt-3 grid grid-cols-2 gap-3 text-sm font-semibold text-gray-700">
              <label className="flex items-center gap-2 rounded-md bg-white border border-gray-200 px-3 py-2"><input type="checkbox" checked={form.enabled} onChange={e => update('enabled', e.target.checked)} /> 启用</label>
              <label className="flex items-center gap-2 rounded-md bg-white border border-gray-200 px-3 py-2"><input type="checkbox" checked={form.autoTransfer} onChange={e => update('autoTransfer', e.target.checked)} /> 自动转存</label>
            </div>
            <button disabled={loading} className="btn-primary mt-3 w-full disabled:opacity-50">{loading ? '添加中...' : `确认添加《${selectedTMDB.title}》`}</button>
          </form>
        )}
      </div>

      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-3">
        {items.length === 0 ? <div className="card col-span-full text-center text-sm text-gray-500">暂无订阅，先从 TMDB 搜索并添加。</div> : items.map(item => (
          <SubscriptionCard key={item.id} item={item} missingCount={subscriptionMissingCount(item, missingBySubscription)} running={running} onRun={run} onRemove={remove} onReload={load} />
        ))}
      </div>

      {result && <div className="card text-sm text-gray-600">本次扫描：成功 {result.success || 0}，失败 {result.failed || 0}</div>}
    </div>
  );
}

function buildSubscriptionMissingMap(missing) {
  const out = {};
  for (const episode of missing || []) {
    const tmdbId = Number(episode.tmdbId || 0);
    if (!tmdbId) continue;
    const season = Number(episode.season || 0);
    out[`${tmdbId}:0`] = (out[`${tmdbId}:0`] || 0) + 1;
    if (season > 0) out[`${tmdbId}:${season}`] = (out[`${tmdbId}:${season}`] || 0) + 1;
  }
  return out;
}

function subscriptionMissingCount(item, missingMap) {
  if (!item || item.mediaType === 'movie' || !item.tmdbId) return 0;
  const season = Number(item.season || 0);
  return missingMap[`${Number(item.tmdbId)}:${season}`] || 0;
}

function SubscriptionCard({ item, missingCount = 0, running, onRun, onRemove, onReload }) {
  const seriesKey = `subscription:${item.id}`;
  const search = useStore(s => s.seriesSearches[seriesKey]);
  const setSeriesSearch = useStore(s => s.setSeriesSearch);
  const [open, setOpen] = useState(false);
  const [activeSource, setActiveSource] = useState('mp');
  const [mpPage, setMpPage] = useState(1);
  const results = item.lastResults || [];
  const locked = results.filter(r => r.source === 'HDHive' && r.hdhiveLocked);
  const points = locked.reduce((sum, r) => sum + (Number(r.unlockPoints) || 0), 0);
  const hdhiveCount = results.filter(r => r.source === 'HDHive').length;
  const pansouCount = results.filter(r => r.source !== 'HDHive').length;
  const pageSize = 20;
  const queryTitle = item.season && item.mediaType !== 'movie' ? `${item.title} S${String(item.season).padStart(2, '0')}` : item.title;
  const seasonCode = item.season && item.mediaType !== 'movie' ? `S${String(item.season).padStart(2, '0')}` : '';
  const searchCodes = seasonCode || item.title;

  const doPanSearch = async () => {
    setActiveSource('pan');
    setSeriesSearch(seriesKey, prev => ({ ...prev, loading: true, codes: searchCodes }));
    try {
      const data = await api('/api/search', { method: 'POST', body: JSON.stringify({ keyword: queryTitle }) });
      setSeriesSearch(seriesKey, prev => ({ ...prev, loading: false, results: data.results || [], query: data.query || queryTitle, codes: searchCodes }));
    } catch (err) {
      setSeriesSearch(seriesKey, prev => ({ ...prev, loading: false, error: err.message, codes: searchCodes }));
    }
  };

  const doHDHiveSearch = async () => {
    setActiveSource('hdhive');
    setSeriesSearch(seriesKey, prev => ({ ...prev, hdhiveLoading: true, codes: searchCodes }));
    try {
      const data = await api('/api/hdhive/search', {
        method: 'POST',
        body: JSON.stringify({ keyword: item.title, mediaType: item.mediaType || 'tv', tmdbId: item.tmdbId || 0 }),
      });
      setSeriesSearch(seriesKey, prev => ({ ...prev, hdhiveLoading: false, results: [...(prev?.results || []), ...(data.results || [])], query: queryTitle, codes: searchCodes }));
    } catch (err) {
      setSeriesSearch(seriesKey, prev => ({ ...prev, hdhiveLoading: false, error: err.message, codes: searchCodes }));
    }
  };

  const doMPSearch = async () => {
    setActiveSource('mp');
    const keywords = [queryTitle];
    if (item.tmdbId) keywords.unshift(`tmdb:${item.tmdbId}`);
    setSeriesSearch(seriesKey, prev => ({ ...prev, mpLoading: true, mpKeywords: keywords }));
    try {
      const body = { keyword: queryTitle };
      if (item.tmdbId) body.tmdbId = String(item.tmdbId);
      const data = await api('/api/mp/search', { method: 'POST', body: JSON.stringify(body) });
      const arr = Array.isArray(data.results) ? data.results : [];
      const mpResults = arr.map(r => ({
        title: r.title || r.description || '',
        description: r.description || '',
        url: r.enclosure || r.page_url || r.torrent_url || r.magnet || '',
        size: r.size ? (r.size >= 1e9 ? (r.size / 1e9).toFixed(1) + 'GB' : r.size >= 1e6 ? (r.size / 1e6).toFixed(0) + 'MB' : r.size + 'B') : '',
        seeders: r.seeders || 0,
        source: r.site_name || r.site || '',
        pubdate: r.pubdate || '',
        raw: r,
      }));
      setSeriesSearch(seriesKey, prev => ({ ...prev, mpLoading: false, mpResults, query: queryTitle, codes: searchCodes }));
      setMpPage(1);
    } catch (err) {
      setSeriesSearch(seriesKey, prev => ({ ...prev, mpLoading: false, mpError: err.message }));
    }
  };

  const doMPDownload = async (resource) => {
    try {
      await api('/api/mp/download', { method: 'POST', body: JSON.stringify({ rawData: resource.raw, tmdbId: String(item.tmdbId || '') }) });
      toast.success('已提交下载');
    } catch (err) {
      toast.error(err.message);
    }
  };

  const allMP = search?.mpResults || [];
  const totalPages = Math.max(1, Math.ceil(allMP.length / pageSize));
  const pageMP = allMP.slice((mpPage - 1) * pageSize, mpPage * pageSize);
  const panResults = search?.results || results;
  const hdhiveResults = panResults.filter(r => r.source === 'HDHive');
  const pansouResults = panResults.filter(r => r.source !== 'HDHive');
  const visibleResults = activeSource === 'hdhive' ? hdhiveResults : pansouResults;

  return (
    <>
      <div onClick={() => setOpen(true)} className="bg-white rounded-lg border border-gray-200 shadow-sm overflow-hidden cursor-pointer hover:shadow-md transition-shadow">
        <div className="relative aspect-[2/3] bg-gray-100">
          {item.posterPath ? <img src={TMDB_IMG + item.posterPath} className="h-full w-full object-cover" alt="" /> : <div className="flex h-full w-full items-center justify-center text-4xl text-gray-300">🎬</div>}
          <div className="absolute inset-0 bg-gradient-to-t from-black/70 via-transparent to-transparent" />
          <span className={`absolute top-2 right-2 px-2 py-0.5 rounded-full text-[10px] font-bold border ${missingCount > 0 ? 'bg-red-50 text-red-700 border-red-200' : locked.length ? 'bg-amber-50 text-amber-700 border-amber-200' : 'bg-emerald-50 text-emerald-700 border-emerald-200'}`}>
            {missingCount > 0 ? `缺${missingCount}集` : locked.length ? `待审${locked.length}` : '已追踪'}
          </span>
          {!item.enabled && <span className="absolute left-2 top-2 rounded-full border border-white/70 bg-white/90 px-2 py-0.5 text-[10px] font-bold text-gray-600">停用</span>}
          {points > 0 && <div className="absolute bottom-8 left-2 rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-[10px] font-black text-amber-700 shadow-sm">{points}积分</div>}
          <div className="absolute bottom-0 left-0 right-0 p-2 text-white">
            <p className="truncate text-xs font-bold">{item.title}</p>
            <p className="mt-0.5 text-[10px] text-white/75">{item.mediaType === 'movie' ? '电影' : '剧集'} · {item.tmdbYear || '未知年份'}{item.season ? ` · S${String(item.season).padStart(2, '0')}` : ''}</p>
          </div>
        </div>
        <div className="px-2.5 py-2">
          <p className="text-xs font-bold text-gray-900 truncate">{item.title}</p>
          <p className="mt-0.5 text-[10px] text-gray-400">{missingCount > 0 ? `缺${missingCount}集 · ` : ''}PanSou {pansouCount} · HDHive {hdhiveCount}</p>
        </div>
      </div>

      {open && (
        <div className="fixed inset-0 z-50 flex items-start justify-center pt-8 px-4 pb-8 overflow-y-auto" onClick={() => setOpen(false)}>
          <div className="fixed inset-0 bg-black/40" />
          <div className="relative bg-white rounded-lg shadow-2xl w-full max-w-5xl my-auto" onClick={e => e.stopPropagation()}>
            <div className="flex items-start gap-3 p-4 border-b border-gray-100">
              {item.posterPath ? <img src={TMDB_IMG + item.posterPath} className="w-14 h-[83px] rounded object-cover shrink-0 bg-gray-100" alt="" /> : <div className="w-14 h-[83px] rounded bg-gray-100 shrink-0 flex items-center justify-center text-xl">🎬</div>}
              <div className="min-w-0 flex-1">
                <h2 className="text-base font-bold text-gray-900">{item.title}</h2>
                <p className="text-xs text-gray-400 mt-0.5">TMDB {item.tmdbId || '未填'}{item.tmdbYear ? ` · ${item.tmdbYear}` : ''}{item.season ? ` · S${String(item.season).padStart(2, '0')}` : ''}</p>
                <div className="mt-1 flex flex-wrap gap-1.5 text-[11px] font-semibold">
                  <span className="rounded bg-gray-100 px-1.5 py-0.5 text-gray-500">PanSou {pansouCount}</span>
                  <span className="rounded bg-gray-100 px-1.5 py-0.5 text-gray-500">HDHive {hdhiveCount}</span>
                  {missingCount > 0 && <span className="rounded bg-red-50 px-1.5 py-0.5 text-red-600">缺集 {missingCount}</span>}
                  {locked.length > 0 && <span className="rounded bg-amber-50 px-1.5 py-0.5 text-amber-700">待审批 {locked.length}</span>}
                </div>
                <p className="mt-1 line-clamp-2 text-xs text-gray-500">{item.overview || item.lastMessage || '点下方按钮搜索资源，支持转存 115、HDHive 解锁和 MoviePilot 下载。'}</p>
              </div>
              <div className="shrink-0 flex items-center gap-1">
                <button title="扫描此订阅" onClick={() => onRun(item.id)} disabled={running} className="p-1 rounded hover:bg-gray-100 disabled:opacity-40">
                  <RefreshCw className={`w-4 h-4 text-gray-400 ${running ? 'animate-spin' : ''}`} />
                </button>
                <button onClick={() => setOpen(false)} className="p-1 rounded hover:bg-gray-100"><X className="w-5 h-5 text-gray-400" /></button>
              </div>
            </div>

            <div className="grid grid-cols-3 border-b border-gray-100 text-sm font-bold">
              <button type="button" onClick={() => setActiveSource('mp')} className={`flex items-center justify-center gap-1.5 px-3 py-3 ${activeSource === 'mp' ? 'bg-primary-50 text-primary-700' : 'text-gray-500 hover:bg-gray-50'}`}>
                <Download className="w-4 h-4" /> MP{allMP.length ? ` · ${allMP.length}` : ''}
              </button>
              <button type="button" onClick={() => setActiveSource('hdhive')} className={`flex items-center justify-center gap-1.5 px-3 py-3 ${activeSource === 'hdhive' ? 'bg-amber-50 text-amber-700' : 'text-gray-500 hover:bg-gray-50'}`}>
                <Sparkles className="w-4 h-4" /> HDHive · {hdhiveResults.length}
              </button>
              <button type="button" onClick={() => setActiveSource('pan')} className={`flex items-center justify-center gap-1.5 px-3 py-3 ${activeSource === 'pan' ? 'bg-primary-50 text-primary-700' : 'text-gray-500 hover:bg-gray-50'}`}>
                <Search className="w-4 h-4" /> 盘搜 · {pansouResults.length}
              </button>
            </div>

            <div className="px-4 pb-4 max-h-[60vh] overflow-y-auto">
              {activeSource === 'mp' && <button type="button" onClick={doMPSearch} disabled={!!search?.mpLoading} className="mb-3 flex w-full items-center justify-center gap-1.5 rounded-md border-2 border-gray-300 px-3 py-2 text-sm font-semibold text-gray-600 hover:border-primary-400 hover:text-primary-600 disabled:opacity-50"><Download className="w-4 h-4" /> {search?.mpLoading ? 'MP搜索中...' : '重新 MP 搜索'}</button>}
              {activeSource === 'hdhive' && <button type="button" onClick={doHDHiveSearch} disabled={!!search?.hdhiveLoading} className="mb-3 flex w-full items-center justify-center gap-1.5 rounded-md border-2 border-amber-300 px-3 py-2 text-sm font-semibold text-amber-700 hover:border-amber-400 hover:bg-amber-50 disabled:opacity-50"><Sparkles className="w-4 h-4" /> {search?.hdhiveLoading ? 'HDHive 搜索中...' : '重新 HDHive 搜索'}</button>}
              {activeSource === 'pan' && <button type="button" onClick={doPanSearch} disabled={!!search?.loading} className="btn-primary mb-3 flex w-full items-center justify-center gap-1.5 text-sm disabled:opacity-50"><Search className="w-4 h-4" /> {search?.loading ? '盘搜中...' : '重新盘搜搜索'}</button>}

              {activeSource === 'mp' && search?.mpKeywords && <div className="flex flex-wrap gap-1 mb-2">{search.mpKeywords.map((kw, i) => <span key={i} className="inline-flex items-center px-2 py-0.5 rounded-full bg-primary-50 text-primary-700 text-[10px] font-semibold border border-primary-200">{kw}</span>)}</div>}
              {activeSource === 'mp' && search?.mpLoading && <p className="text-sm text-gray-400 py-2">MP搜索中...</p>}
              {activeSource === 'hdhive' && search?.hdhiveLoading && <p className="text-sm text-gray-400 py-2">HDHive 搜索中...</p>}
              {activeSource === 'pan' && search?.loading && <p className="text-sm text-gray-400 py-2">盘搜中...</p>}
              {activeSource === 'mp' && search?.mpError && <div className="rounded-md bg-red-50 border border-red-200 p-2.5 mb-2"><p className="text-sm font-semibold text-red-600">{search.mpError}</p></div>}
              {activeSource !== 'mp' && search?.error && <div className="rounded-md bg-red-50 border border-red-200 p-2.5 mb-2"><p className="text-sm font-semibold text-red-600">{search.error}</p></div>}

              {activeSource === 'mp' && allMP.length > 0 && (
                <div className="mb-3">
                  <div className="mb-2 flex items-center justify-between">
                    <p className="text-xs font-bold text-gray-400">MP结果 · {allMP.length} 条{totalPages > 1 ? ` · ${mpPage}/${totalPages}页` : ''}</p>
                    {totalPages > 1 && <div className="flex gap-1"><button type="button" onClick={() => setMpPage(p => Math.max(1, p - 1))} disabled={mpPage <= 1} className="text-[10px] font-semibold px-2 py-0.5 rounded border border-gray-200 disabled:opacity-30 hover:bg-gray-100">上一页</button><button type="button" onClick={() => setMpPage(p => Math.min(totalPages, p + 1))} disabled={mpPage >= totalPages} className="text-[10px] font-semibold px-2 py-0.5 rounded border border-gray-200 disabled:opacity-30 hover:bg-gray-100">下一页</button></div>}
                  </div>
                  <div className="space-y-1.5">
                    {pageMP.map((resource, index) => <div key={`mp-${index}`} className="rounded-md p-2 border border-gray-100 bg-gray-50"><div className="flex items-start justify-between gap-2"><div className="min-w-0 flex-1"><p className="text-sm font-medium text-gray-700">{resource.title}</p>{resource.description && <p className="text-xs text-gray-400 mt-0.5 line-clamp-2">{resource.description}</p>}<p className="text-xs text-gray-400 mt-0.5">{resource.source}{resource.size ? ` · ${resource.size}` : ''}{resource.seeders ? ` · ${resource.seeders}↑` : ''}</p></div><button type="button" onClick={() => doMPDownload(resource)} className="shrink-0 text-xs font-semibold text-primary-600 hover:text-primary-700">下载</button></div></div>)}
                  </div>
                </div>
              )}
              {activeSource === 'mp' && allMP.length === 0 && !search?.mpLoading && search?.mpResults !== undefined && <p className="text-xs text-gray-400 py-2">MP无匹配结果</p>}

              {activeSource !== 'mp' && visibleResults.length > 0 && <SearchResults search={{ ...(search || {}), results: visibleResults, query: search?.query || queryTitle, codes: searchCodes }} targetCid={item.targetCid || ''} subscriptionId={item.id} onUnlocked={onReload} />}
              {activeSource !== 'mp' && visibleResults.length === 0 && !search?.loading && !search?.hdhiveLoading && <p className="text-sm text-gray-400 py-3 text-center">这一栏还没有结果，点击上方按钮搜索。</p>}
              {!search && results.length === 0 && activeSource === 'mp' && <p className="text-sm text-gray-400 py-3 text-center">还没有候选资源，点击上方按钮搜索或扫描此订阅。</p>}
            </div>

            <div className="flex items-center justify-between gap-2 border-t border-gray-100 px-4 py-3">
              <p className="text-[11px] text-gray-400">上次：{item.lastRunAt ? new Date(item.lastRunAt).toLocaleString('zh-CN') : '未扫描'}{item.autoTransfer ? ' · 自动转存' : ''}</p>
              <button onClick={() => onRemove(item.id)} className="inline-flex items-center gap-1 rounded-md border border-red-200 px-2.5 py-1.5 text-xs font-semibold text-red-500 hover:bg-red-50"><Trash2 className="h-3.5 w-3.5" /> 删除</button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

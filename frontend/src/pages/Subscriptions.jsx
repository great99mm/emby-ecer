import { useEffect, useState } from 'react';
import { api } from '../api';
import toast from 'react-hot-toast';
import { CheckCircle2, ExternalLink, Play, Plus, Search, Trash2, RefreshCw } from 'lucide-react';

const emptyForm = { title: '', mediaType: 'tv', tmdbId: '', season: '', enabled: true, autoTransfer: false, targetCid: '0' };
const TMDB_IMG = 'https://image.tmdb.org/t/p/w342';

export default function Subscriptions() {
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
    <div className="space-y-5">
      <div className="card">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h1 className="text-xl font-black text-gray-900">订阅与自动扫描</h1>
            <p className="mt-1 text-sm text-gray-500">按标题/TMDB 追踪 HDHive 115 资源，可结合大模型自动排序候选。</p>
          </div>
          <button onClick={() => run()} disabled={running || !items.length} className="btn-primary flex items-center gap-2 text-sm disabled:opacity-50">
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

      <div className="space-y-3">
        {items.length === 0 ? <div className="card text-center text-sm text-gray-500">暂无订阅</div> : items.map(item => (
          <div key={item.id} className="card">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <h2 className="truncate text-base font-bold text-gray-900">{item.title}</h2>
                  <span className="rounded-full bg-gray-100 px-2 py-0.5 text-[11px] font-bold text-gray-500">{item.mediaType === 'movie' ? '电影' : '剧集'}</span>
                  {!item.enabled && <span className="rounded-full bg-amber-50 px-2 py-0.5 text-[11px] font-bold text-amber-600">停用</span>}
                </div>
                <p className="mt-1 text-xs text-gray-400">TMDB {item.tmdbId || '未填'} · 上次：{item.lastRunAt ? new Date(item.lastRunAt).toLocaleString('zh-CN') : '未扫描'}</p>
                {item.lastMessage && <p className="mt-1 text-xs text-gray-500">{item.lastMessage}</p>}
              </div>
              <div className="shrink-0 flex items-center gap-1">
                <button onClick={() => run(item.id)} disabled={running} className="rounded-md border border-gray-300 px-2.5 py-1.5 text-xs font-semibold text-gray-600 hover:text-primary-600 disabled:opacity-40">扫描</button>
                <button onClick={() => remove(item.id)} className="rounded-md border border-red-200 px-2.5 py-1.5 text-xs font-semibold text-red-500 hover:bg-red-50"><Trash2 className="h-3.5 w-3.5" /></button>
              </div>
            </div>
            {!!item.lastResults?.length && <div className="mt-3 space-y-2 border-t border-gray-100 pt-3">
              {item.lastResults.slice(0, 3).map((r, i) => <div key={i} className="rounded-md bg-gray-50 px-3 py-2 text-xs text-gray-600"><b>{r.title}</b><div className="mt-0.5 text-gray-400">{r.source}</div></div>)}
            </div>}
          </div>
        ))}
      </div>

      {result && <div className="card text-sm text-gray-600">本次扫描：成功 {result.success || 0}，失败 {result.failed || 0}</div>}
    </div>
  );
}

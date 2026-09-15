import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import useStore from '../store';
import { api } from '../api';
import toast from 'react-hot-toast';
import { Search, X, RefreshCw, Film, ArrowUpRight, Check, Download, ChevronLeft, ChevronRight, Loader2 } from 'lucide-react';
import Modal from './Modal';

const TMDB_IMG = 'https://image.tmdb.org/t/p/w342';

export default function MissingCard({ group, selectable = false, selected = false, onToggleSelect, onIgnore, onIgnoreEpisode }) {
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [selectedSeason, setSelectedSeason] = useState(null);
  const [mpPage, setMpPage] = useState(1);
  const [downloads, setDownloads] = useState({});
  const seasonBuckets = {};
  for (const item of group.items || []) {
    const season = item.season || 0;
    if (!seasonBuckets[season]) seasonBuckets[season] = [];
    if (item.episode) seasonBuckets[season].push(item.episode);
  }
  const seasonKeys = Object.keys(seasonBuckets).filter(k => Number(k) > 0).sort((a,b) => seasonBuckets[b].length - seasonBuckets[a].length);
  const searchSeason = selectedSeason != null && seasonBuckets[selectedSeason] ? selectedSeason : Number(seasonKeys[0] || 0);
  const searchEpisodes = seasonBuckets[searchSeason] || [];
  const codeList = (group.items || []).filter(item => item.season === searchSeason).map(item => item.code);
  const seriesKey = `series:${group.tmdbId || group.key}:s${searchSeason}`;
  const search = useStore(s => s.seriesSearches[seriesKey]);
  const setSeriesSearch = useStore(s => s.setSeriesSearch);
  const setActiveJobId = useStore(s => s.setActiveJobId);
  const setJobStatus = useStore(s => s.setJobStatus);
  const jobStatus = useStore(s => s.jobStatus);
  const mpReady = useStore(s => !!s.settings.ready?.mp);
  const busy = jobStatus && !['done','error'].includes(jobStatus.status);
  const totalEps = group.totalEpisodes || 0;
  const ownedEps = group.ownedEpisodes || 0;
  const healthPct = totalEps > 0 ? Math.min(100, Math.round(ownedEps / totalEps * 100)) : 0;
  const missingEps = (group.items || []).length;
  const pageSize = 20;

  const rescanSeries = async () => {
    if (!group.embySeriesId) {
      toast.error('缺少 Emby 剧集 ID，无法单独扫描');
      return;
    }
    try {
      const data = await api('/api/jobs', { method: 'POST', body: JSON.stringify({ type: 'scan', airedOnly: true, seriesId: group.embySeriesId }) });
      setActiveJobId(data.jobId);
      setJobStatus({ status: 'running', progress: 0, message: `正在重新扫描《${group.title}》...`, current: group.title });
      toast.success('已开始单剧扫描');
    } catch (err) {
      toast.error(err.message);
    }
  };

  const ignoreEpisode = async (item) => {
    if (!group.embySeriesId) return toast.error('缺少 Emby 剧集 ID，无法忽略单集');
    try {
      await api('/api/episode-ignores', {
        method: 'POST',
        body: JSON.stringify({
          items: [{
            seriesId: group.embySeriesId,
            seriesName: group.title,
            season: item.season,
            episode: item.episode,
            code: item.code,
            title: item.episodeName || '',
          }],
        }),
      });
      onIgnoreEpisode?.(item);
      toast.success(`已忽略 ${item.code}`);
    } catch (err) {
      toast.error(err.message);
    }
  };

  const doMPSearch = async () => {
    setMpPage(1);
    const keywords = [group.title];
    if (group.tmdbId) keywords.unshift(`tmdb:${group.tmdbId}`);
    setSeriesSearch(seriesKey, prev => ({ ...prev, mpLoading: true, mpError: '', mpKeywords: keywords }));
    try {
      const body = { keyword: group.title, season: searchSeason, episodes: searchEpisodes };
      if (group.tmdbId) body.tmdbId = String(group.tmdbId);
      const data = await api('/api/mp/search', { method: 'POST', body: JSON.stringify(body) });
      const arr = Array.isArray(data.results) ? data.results : [];
      const items = arr.map(r => ({
        title: r.title || r.description || '',
        description: r.description || '',
        url: r.enclosure || r.page_url || r.torrent_url || r.magnet || '',
        size: r.size ? (r.size >= 1e9 ? (r.size/1e9).toFixed(1)+'GB' : r.size >= 1e6 ? (r.size/1e6).toFixed(0)+'MB' : r.size+'B') : '',
        seeders: r.seeders || 0,
        source: r.site_name || r.site || '',
        pubdate: r.pubdate || '',
        match: r.match || null,
        raw: r,
      }));
      setSeriesSearch(seriesKey, prev => ({ ...prev, mpLoading: false, mpResults: items, mpError: (data.errors || []).join('；'), query: group.title }));
    } catch (err) {
      setSeriesSearch(seriesKey, prev => ({ ...prev, mpLoading: false, mpError: err.message }));
    }
  };

  const doMPDownload = async (item) => {
    const key = item.url || item.title;
    setDownloads(prev => ({ ...prev, [key]: { loading: true } }));
    try {
      await api('/api/mp/download', { method: 'POST', body: JSON.stringify({ rawData: item.raw, tmdbId: String(group.tmdbId || '') }) });
      setDownloads(prev => ({ ...prev, [key]: { done: true } }));
      toast.success('已提交 MoviePilot 下载');
    } catch (err) {
      setDownloads(prev => ({ ...prev, [key]: { error: err.message } }));
      toast.error(err.message);
    }
  };

  const loadMPSubscribeStatus = async () => {
    setSeriesSearch(seriesKey, prev => ({ ...prev, mpSubscribeLoading: true, mpSubscribeError: '' }));
    try {
      const data = await api('/api/mp/subscribe/status', {
        method: 'POST',
        body: JSON.stringify({ tmdbId: String(group.tmdbId), mediaType: 'tv', season: searchSeason }),
      });
      setSeriesSearch(seriesKey, prev => ({ ...prev, mpSubscribeLoading: false, mpSubscribeStatus: data }));
    } catch (err) {
      setSeriesSearch(seriesKey, prev => ({ ...prev, mpSubscribeLoading: false, mpSubscribeError: err.message }));
    }
  };

  const doMPSubscribe = async () => {
    setSeriesSearch(seriesKey, prev => ({ ...prev, mpSubscribeSending: true, mpSubscribeError: '' }));
    try {
      await api('/api/mp/subscribe', {
        method: 'POST',
        body: JSON.stringify({ tmdbId: String(group.tmdbId), mediaType: 'tv', season: searchSeason, title: group.title }),
      });
      toast.success('已发送到 MoviePilot 订阅');
      await loadMPSubscribeStatus();
    } catch (err) {
      setSeriesSearch(seriesKey, prev => ({ ...prev, mpSubscribeError: err.message }));
      toast.error(err.message);
    } finally {
      setSeriesSearch(seriesKey, prev => ({ ...prev, mpSubscribeSending: false }));
    }
  };

  const matchCode = (r) => {
    // 后端已经解析过集号并打分，优先用它的判断
    const m = r.match;
    if (m) {
      if (m.matchedEpisodes?.length) return 'match';
      if (m.episodes?.length) return false;
      if (m.seasonPack || m.seasonMatched) return 'include';
    }
    const t = (r.title || '').toUpperCase();
    const d = (r.description || '').toUpperCase();
    const combined = t + ' ' + d;
    // 精确匹配集号
    if (codeList.some(c => t.includes(c.toUpperCase()) || d.includes(c.toUpperCase()))) return 'match';
    // 描述中包含范围（如"第1-12集"、"全30集"），且缺的集在范围内
    const desc = r.description || r.title || '';
    const rangeMatch = desc.match(/第\s*(\d+)\s*[-~～]\s*(\d+)\s*集/) || 
                       desc.match(/全\s*(\d+)\s*集/);
    if (rangeMatch) {
      let start = 1, end = 0;
      if (rangeMatch[2]) { start = parseInt(rangeMatch[1]); end = parseInt(rangeMatch[2]); }
      else { end = parseInt(rangeMatch[1]); }
      if (end > 0 && codeList.some(c => { const m = c.match(/S\d+E(\d+)/i); return m && parseInt(m[1]) >= start && parseInt(m[1]) <= end; })) {
        return 'include';
      }
    }
    // S01 季匹配
    if (codeList.some(c => { const s = c.match(/S(\d+)/i); return s && combined.includes('S' + s[1]); })) return 'include';
    return false;
  };

  const MatchTags = ({ result }) => {
    const m = result.match;
    if (!m) return null;
    const chips = [];
    if (m.matchedEpisodes?.length) chips.push({ text: `命中 E${m.matchedEpisodes.join('/E')}`, tone: 'bg-emerald-100 text-emerald-700' });
    else if (m.seasonPack) chips.push({ text: '整季包', tone: 'bg-amber-100 text-amber-700' });
    if (m.free) chips.push({ text: '免费', tone: 'bg-blue-100 text-blue-700' });
    else if (m.discount) chips.push({ text: m.discount, tone: 'bg-blue-50 text-blue-600' });
    if (m.hitAndRun) chips.push({ text: 'H&R', tone: 'bg-red-100 text-red-700' });
    for (const tag of m.tags || []) chips.push({ text: tag, tone: 'bg-gray-100 text-gray-600' });
    if (!chips.length) return null;
    return (
      <div className="mt-1 flex flex-wrap gap-1">
        {chips.map((chip, i) => (
          <span key={i} className={`inline-flex items-center rounded px-1.5 py-0.5 text-[11px] font-bold ${chip.tone}`}>{chip.text}</span>
        ))}
      </div>
    );
  };

  const allMP = search?.mpResults || [];
  const totalPages = Math.max(1, Math.ceil(allMP.length / pageSize));
  const currentPage = Math.min(mpPage, totalPages);
  const pageMP = allMP.slice((currentPage - 1) * pageSize, currentPage * pageSize);
  const matchedMP = pageMP.filter(r => matchCode(r) !== false);
  const unmatchedMP = pageMP.filter(r => matchCode(r) === false);
  const renderTorrent = (result, i) => {
    const state = downloads[result.url || result.title];
    const matched = matchCode(result);
    return <div key={(result.url || result.title) + i} className={`result-row ${matched ? 'is-match' : ''}`}><div className="flex flex-col gap-3 sm:flex-row sm:items-start"><div className="min-w-0 flex-1"><p className="break-words text-sm font-medium leading-6">{result.title}</p>{result.description && <p className="mt-1 line-clamp-2 break-words text-xs leading-5 text-gray-500">{result.description}</p>}<p className="mt-1 text-xs text-gray-400">{result.source || 'MoviePilot'}{result.size ? ` · ${result.size}` : ''}{result.seeders ? ` · ${result.seeders} 做种` : ''}</p><MatchTags result={result} /></div><button onClick={() => doMPDownload(result)} disabled={state?.loading || state?.done} className="btn-outline shrink-0 !min-h-9 !px-3 !py-1.5 !text-xs">{state?.loading ? <Loader2 size={14} className="animate-spin" /> : state?.done ? <Check size={14} /> : <Download size={14} />}{state?.loading ? '提交中' : state?.done ? '已发送' : '下载'}</button></div>{state?.error && <p className="mt-2 break-words text-xs text-red-600">{state.error}</p>}</div>;
  };
  return (
    <>
      <article className={`series-card ${selected ? 'is-selected' : ''}`}>
        <button onClick={() => selectable ? onToggleSelect?.(group) : setOpen(true)} aria-label={`${selectable ? '选择' : '查看'} ${group.title}`} aria-pressed={selectable ? selected : undefined} className="block w-full text-left">
          <div className="relative aspect-[3/4] overflow-hidden bg-gray-100">
            {group.posterPath ? <img src={TMDB_IMG + group.posterPath} loading="lazy" alt="" className="h-full w-full object-cover" /> : <div className="poster-placeholder h-full w-full"><Film size={44} strokeWidth={1} /></div>}
            <span className="absolute right-2.5 top-2.5 rounded-lg bg-white/95 px-2 py-1 text-[11px] font-medium text-amber-700">缺 {missingEps} 集</span>
            {selectable && <span className={`absolute left-2.5 top-2.5 flex h-6 w-6 items-center justify-center rounded-md border border-white ${selected ? 'bg-primary-600 text-white' : 'bg-white/90'}`}>{selected && <Check size={15} />}</span>}
            <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/45 to-transparent px-3 pb-2 pt-8"><p className="text-[11px] font-medium text-white">{totalEps ? `${ownedEps} / ${totalEps} 集 · ${healthPct}%` : '等待补齐'}</p></div>
          </div>
          <div className="px-3 pb-3.5 pt-3"><h3 className="line-clamp-2 min-h-10 text-sm font-medium leading-5">{group.title}</h3><div className="mt-2 flex items-center justify-between text-[11px] text-gray-400"><span>{seasonKeys.length ? `${seasonKeys.length} 季存在缺集` : `${missingEps} 集待补齐`}</span><ArrowUpRight size={14} /></div>{totalEps > 0 && <div className="mt-3 h-1 overflow-hidden rounded-full bg-gray-100"><div className="h-full rounded-full bg-primary-400" style={{width: `${healthPct}%`}} /></div>}</div>
        </button>
        {!selectable && onIgnore && <button onClick={() => onIgnore(group)} className="absolute left-2.5 top-2.5 rounded-lg bg-black/25 px-2 py-1 text-[11px] text-white backdrop-blur-sm hover:bg-black/50" aria-label={`忽略 ${group.title}`}>忽略</button>}
      </article>
      {open && <Modal title={group.title} description={`缺 ${missingEps} 集${totalEps ? ` · 已有 ${ownedEps} / ${totalEps} 集` : ''}`} onClose={() => setOpen(false)}>
        <div className="space-y-6">
          <div className="flex items-start justify-between gap-3"><div className="flex flex-wrap items-center gap-2"><span className="pill">TMDB {group.tmdbId || '未匹配'}</span><span className="pill-amber">待补齐 {missingEps} 集</span></div><button onClick={rescanSeries} disabled={busy || !group.embySeriesId} className="btn-ghost !min-h-8 !py-1 !text-xs"><RefreshCw size={14} />单剧重扫</button></div>
          <section><div className="mb-3 flex flex-wrap items-center justify-between gap-3"><h3 className="text-sm font-medium">缺失集号</h3>{seasonKeys.length > 0 && <select aria-label="选择搜索季" value={searchSeason} onChange={e => { setSelectedSeason(Number(e.target.value)); setMpPage(1); }} className="field !min-h-9 !w-auto !py-1.5 !text-xs">{[...seasonKeys].sort((a,b) => Number(a)-Number(b)).map(key => <option key={key} value={key}>第 {key} 季 · 缺 {seasonBuckets[key].length} 集</option>)}</select>}</div><div className="flex flex-wrap gap-2">{(group.items || []).filter(item => !searchSeason || item.season === searchSeason).map(item => <span key={item.id || item.code} className="inline-flex items-center gap-1 rounded-lg border border-amber-100 bg-amber-50 px-2.5 py-1.5 text-xs font-medium text-amber-800">{item.code}<button onClick={() => ignoreEpisode(item)} aria-label={`忽略 ${item.code}`} className="ml-1 rounded p-0.5 text-amber-500 hover:bg-amber-100 hover:text-amber-800"><X size={12} /></button></span>)}</div></section>
          <section className="rounded-2xl border border-gray-200 p-4 sm:p-5">
            <div className="mb-4"><h3 className="text-sm font-semibold">通过 MoviePilot 补齐</h3><p className="mt-1 text-xs leading-6 text-gray-500">{searchSeason ? `搜索第 ${searchSeason} 季，优先展示命中缺集的资源。` : '搜索资源，或将剧集交给 MoviePilot 持续追踪。'}</p></div>
            {mpReady ? <div className="flex flex-wrap gap-2"><button onClick={doMPSearch} disabled={!!search?.mpLoading} className="btn-primary"><Search size={16} />{search?.mpLoading ? '搜索中…' : '搜索资源'}</button><button onClick={doMPSubscribe} disabled={!group.tmdbId || !!search?.mpSubscribeSending} className="btn-outline">{search?.mpSubscribeSending ? '发送中…' : '发送到 MP 订阅'}</button><button onClick={loadMPSubscribeStatus} disabled={!group.tmdbId || !!search?.mpSubscribeLoading} className="btn-ghost">{search?.mpSubscribeLoading ? '查询中…' : '查询订阅'}</button></div> : <button onClick={() => { setOpen(false); navigate('/settings'); }} className="btn-primary">连接 MoviePilot<ArrowUpRight size={16} /></button>}
            {search?.mpSubscribeStatus && <p className="mt-3 text-xs text-primary-600">{search.mpSubscribeStatus.exists ? 'MoviePilot 已有当前季的订阅' : 'MoviePilot 暂无当前季的订阅'}</p>}
            {search?.mpSubscribeError && <p className="mt-3 break-words text-xs text-red-600">{search.mpSubscribeError}</p>}
          </section>
          {search?.mpError && <p role="alert" className="rounded-xl bg-red-50 p-4 text-sm text-red-600">{search.mpError}</p>}
          {search?.mpLoading ? <div className="flex items-center justify-center gap-2 py-8 text-sm text-gray-500" role="status"><Loader2 size={18} className="animate-spin" />正在搜索站点资源…</div> : search?.mpResults !== undefined && <section className="space-y-4"><div className="flex items-center justify-between"><h3 className="text-sm font-semibold">搜索结果 <span className="ml-1 text-xs font-normal text-gray-400">{allMP.length} 条</span></h3>{totalPages > 1 && <div className="flex items-center gap-2 text-xs text-gray-500"><button onClick={() => setMpPage(p => Math.max(1,p-1))} disabled={currentPage <= 1} className="icon-button !h-8 !w-8" aria-label="上一页"><ChevronLeft size={16} /></button>{currentPage} / {totalPages}<button onClick={() => setMpPage(p => Math.min(totalPages,p+1))} disabled={currentPage >= totalPages} className="icon-button !h-8 !w-8" aria-label="下一页"><ChevronRight size={16} /></button></div>}</div>{matchedMP.length > 0 && <div className="space-y-2"><p className="text-xs font-medium text-primary-600">命中缺集 · {matchedMP.length} 条</p>{matchedMP.map(renderTorrent)}</div>}{unmatchedMP.length > 0 && <details open={!matchedMP.length}><summary className="mb-3 text-xs text-gray-500">其他资源 · {unmatchedMP.length} 条</summary><div className="space-y-2">{unmatchedMP.map(renderTorrent)}</div></details>}{!allMP.length && <div className="empty-state !py-8"><Search size={22} className="mb-3 text-gray-300" /><p className="text-sm text-gray-500">暂未找到资源，可以发送到 MP 订阅继续追踪。</p></div>}</section>}
        </div>
      </Modal>}
    </>
  );
}

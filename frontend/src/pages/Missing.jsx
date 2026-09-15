import { useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import Modal from '../components/Modal';
import ScanDiagnostics from '../components/ScanDiagnostics';
import useStore, { isScanBusy } from '../store';
import { api } from '../api';
import toast from 'react-hot-toast';
import { Radar, Search, Library, ArrowRight, RefreshCw, CheckSquare, ShieldCheck, ScanLine, Check } from 'lucide-react';
import ProgressBar from '../components/ProgressBar';
import MissingCard from '../components/MissingCard';
import StatCard from '../components/StatCard';

export default function Missing() {
  const [params] = useSearchParams();
  const [query, setQuery] = useState(params.get('title') || '');
  const [sortBy, setSortBy] = useState('missing');
  const [visibleCount, setVisibleCount] = useState(50);
  useEffect(() => { setVisibleCount(50); }, [query, sortBy]);
  const ready = useStore(s => s.settings.ready || {});
  const scanReady = ready.emby && ready.tmdb;
  const missing = useStore(s => s.missing);
  const scan = useStore(s => s.scan);
  const submitScan = useStore(s => s.startScan);
  const setScan = useStore(s => s.setScan);
  const busy = useStore(isScanBusy);
  const [selectMode, setSelectMode] = useState(false);
  const [selected, setSelected] = useState({});
  const [exemptions, setExemptions] = useState({ manual: [], complete: [] });
  const [episodeIgnores, setEpisodeIgnores] = useState([]);
  const [exemptionsOpen, setExemptionsOpen] = useState(false);
  const [exemptionTab, setExemptionTab] = useState('manual');
  const [selectedExemptions, setSelectedExemptions] = useState({});
  const [verifying, setVerifying] = useState(false);

  const episodeExemptions = episodeIgnores.map(item => ({
    id: item.key,
    name: `${item.seriesName || '未知剧集'} · ${item.code}`,
    tmdbName: item.title || '',
    episode: true,
  }));
  const activeExemptions = exemptionTab === 'manual' ? exemptions.manual
    : exemptionTab === 'complete' ? exemptions.complete
    : episodeExemptions;

  // Group + compute health
  const groupList = useMemo(() => {
  const groups = {};
  for (const item of missing) {
    const key = `${item.tmdbId || 0}:${item.officialTitle || item.embyTitle}`;
    if (!groups[key]) {
      groups[key] = {
        key,
        title: item.officialTitle || item.embyTitle,
        tmdbId: item.tmdbId,
        tmdbYear: item.tmdbMatchYear,
        embySeriesId: item.embySeriesId,
        posterPath: item.posterPath,
        totalEpisodes: item.totalEpisodes || 0,
        ownedEpisodes: item.ownedEpisodes || 0,
        codes: [item.code],
        items: [item],
      };
    } else {
      groups[key].codes.push(item.code);
      groups[key].items.push(item);
    }
  }
  return Object.values(groups);
  }, [missing]);
  const visibleGroups = useMemo(() => groupList.filter(g => (g.title + ' ' + g.tmdbId).toLowerCase().includes(query.trim().toLowerCase())).sort((a,b) => sortBy === 'title' ? a.title.localeCompare(b.title, 'zh-CN') : b.items.length - a.items.length), [groupList, query, sortBy]);

  // Overall health
  const totalMissing = missing.length;
  const totalTMDB = groupList.reduce((s, g) => s + g.totalEpisodes, 0);
  const totalOwned = groupList.reduce((s, g) => s + g.ownedEpisodes, 0);
  const healthPct = totalTMDB > 0 ? Math.round((totalOwned / totalTMDB) * 100) : 0;

  const startScan = async (recentOnly = false) => {
    try {
      await submitScan({ recentOnly });
    } catch (err) {
      toast.error(err.message);
    }
  };

  const loadExemptions = async () => {
    try {
      const data = await api('/api/exemptions');
      setExemptions({ manual: data.manual || [], complete: data.complete || [] });
    } catch {}
  };

  const loadEpisodeIgnores = async () => {
    try {
      const data = await api('/api/episode-ignores');
      setEpisodeIgnores(data.items || []);
    } catch {}
  };

  useEffect(() => { loadExemptions(); loadEpisodeIgnores(); }, []);

  const verifyGaps = async () => {
    setVerifying(true);
    try {
      const data = await api('/api/scan/verify', { method: 'POST' });
      if (data.scan) setScan(data.scan);
      toast.success(data.removed ? `已补齐 ${data.removed} 集，列表已更新` : '校验完成，缺集列表没有变化');
    } catch (err) {
      toast.error(err.message);
    } finally {
      setVerifying(false);
    }
  };

  const dropMissingEpisode = (item) => {
    if (!scan) return;
    setScan({
      ...scan,
      missing: (scan.missing || []).filter(m => !(m.embySeriesId === item.embySeriesId && m.season === item.season && m.episode === item.episode)),
    });
    loadEpisodeIgnores();
  };

  const openExemptionModal = (tab = 'manual') => {
    setExemptionTab(tab);
    setSelectedExemptions({});
    setExemptionsOpen(true);
  };

  const closeExemptionModal = () => {
    setExemptionsOpen(false);
    setSelectedExemptions({});
  };

  const toExemptionItem = (group) => ({
    id: group.embySeriesId,
    name: group.title,
    tmdbId: group.tmdbId || 0,
    tmdbName: group.title,
    tmdbYear: group.tmdbYear || '',
  });

  const toggleGroup = (group) => {
    if (!group.embySeriesId) return;
    setSelected(prev => {
      const next = { ...prev };
      if (next[group.embySeriesId]) delete next[group.embySeriesId];
      else next[group.embySeriesId] = group;
      return next;
    });
  };

  const selectedGroups = Object.values(selected);

  const startSelectedScan = async () => {
    const ids = selectedGroups.map(g => g.embySeriesId).filter(Boolean);
    if (!ids.length) return toast.error('请先选择剧集');
    try {
      await submitScan({ seriesIds: ids }, `正在单独扫描 ${ids.length} 部剧…`);
      setSelectMode(false);
      setSelected({});
    } catch (err) {
      toast.error(err.message);
    }
  };

  const addGroupsToIgnore = async (groupsToIgnore) => {
    const items = groupsToIgnore.filter(g => g.embySeriesId).map(toExemptionItem);
    if (!items.length) return toast.error('没有可加入忽略的剧集');
    if (!window.confirm(`确认将 ${items.length} 部剧加入免检名单？以后扫描会跳过这些剧。`)) return;
    try {
      const data = await api('/api/exemptions', { method: 'POST', body: JSON.stringify({ items }) });
      setExemptions({ manual: data.manual || [], complete: data.complete || [] });
      const ignored = new Set(items.map(i => i.id));
      if (scan) setScan({ ...scan, missing: (scan.missing || []).filter(item => !ignored.has(item.embySeriesId)) });
      setSelected({});
      setSelectMode(false);
      toast.success('已加入免检名单');
    } catch (err) {
      toast.error(err.message);
    }
  };

  const selectedExemptionItems = Object.values(selectedExemptions);
  const toggleExemption = (item) => {
    setSelectedExemptions(prev => {
      const next = { ...prev };
      if (next[item.id]) delete next[item.id];
      else next[item.id] = item;
      return next;
    });
  };
  const deleteExemptions = async (items) => {
    const ids = items.map(i => i.id).filter(Boolean);
    if (!ids.length) return;
    try {
      if (exemptionTab === 'episode') {
        const data = await api('/api/episode-ignores/delete', { method: 'POST', body: JSON.stringify({ keys: ids }) });
        setEpisodeIgnores(data.items || []);
      } else {
        const data = await api('/api/exemptions/delete', { method: 'POST', body: JSON.stringify({ ids }) });
        setExemptions({ manual: data.manual || [], complete: data.complete || [] });
      }
      setSelectedExemptions({});
      toast.success('已从免检名单移除');
    } catch (err) {
      toast.error(err.message);
    }
  };

  return (
    <div className="page-stack">
      <div className="page-heading">
        <div><p className="eyebrow mb-2">LIBRARY / MISSING</p><h1 className="page-title">缺集列表</h1><p className="page-description">按剧集整理缺口，找到资源就能开始补齐。</p></div>
        <div className="flex flex-wrap gap-2">
          <button onClick={verifyGaps} disabled={busy || verifying || !missing.length || !scanReady} className="btn-primary"><ScanLine size={16} className={verifying ? 'animate-pulse' : ''} />{verifying ? '校验中…' : '快速校验'}</button>
          <button onClick={() => startScan(false)} disabled={busy || !scanReady} className="btn-outline"><RefreshCw size={15} />重新扫描</button>
        </div>
      </div>
      <ProgressBar />
      {(scan?.summary?.seriesNeedsReview ?? 0) > 0 && <p className="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-800">{scan.summary.seriesNeedsReview} 部剧与 TMDB 编号不同，已单独检查资源编号。断号结果见下方，TMDB 缺集数仍待确认。<button type="button" className="ml-2 underline" onClick={() => { const section = document.getElementById('scan-diagnostics'); if (section) { section.open = true; section.scrollIntoView({ behavior: 'smooth', block: 'start' }); } }}>查看编号检查</button></p>}
      {scan?.scannedAt && <div className="metrics-strip">
        <StatCard label="待补齐集数" value={totalMissing} note="已播出但尚未入库" accent={totalMissing > 0} />
        <StatCard label="涉及剧集" value={groupList.length} note="点击海报查看详情" />
        <StatCard label="已有集数" value={totalOwned} note="当前所列剧集" />
        <StatCard label="剧集完整度" value={totalTMDB ? `${healthPct}%` : '—'} note="当前所列剧集" />
      </div>}
      <section>
        <div className="mb-5 flex flex-col justify-between gap-3 lg:flex-row lg:items-center">
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <label className="relative min-w-0 flex-1 lg:max-w-xs"><Search size={16} className="absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400" /><input aria-label="搜索缺集剧名" value={query} onChange={e => setQuery(e.target.value)} placeholder="搜索剧名或 TMDB ID" className="field !pl-10" /></label>
            <select aria-label="剧集排序" className="field !w-auto !max-w-[140px]" value={sortBy} onChange={e => setSortBy(e.target.value)}><option value="missing">缺集最多</option><option value="title">剧名排序</option></select>
          </div>
          <div className="flex flex-wrap items-center gap-1">
            <button onClick={() => openExemptionModal('manual')} className="btn-ghost"><ShieldCheck size={16} />免检名单<span className="pill !py-0.5">{exemptions.manual.length + exemptions.complete.length + episodeIgnores.length}</span></button>
            <button onClick={() => { setSelectMode(v => !v); setSelected({}); }} disabled={!groupList.length} className="btn-ghost"><CheckSquare size={16} />{selectMode ? '完成多选' : '批量管理'}</button>
          </div>
        </div>
        {selectMode && <div className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-primary-200 bg-primary-50 px-4 py-3"><p className="text-sm text-primary-700">已选 {selectedGroups.length} 部剧</p><div className="flex flex-wrap gap-2"><button onClick={startSelectedScan} disabled={busy || !selectedGroups.length || !scanReady} className="btn-primary !min-h-9 !py-1.5">单独扫描</button><button onClick={() => addGroupsToIgnore(selectedGroups)} disabled={!selectedGroups.length} className="btn-outline !min-h-9 !py-1.5">加入免检</button></div></div>}
        {visibleGroups.length ? <><p className="mb-4 text-xs text-gray-400">{query ? `找到 ${visibleGroups.length} 部剧集` : `${groupList.length} 部剧集待补齐`}</p><div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">{visibleGroups.slice(0, visibleCount).map(group => <MissingCard key={group.key} group={group} selectable={selectMode} selected={!!selected[group.embySeriesId]} onToggleSelect={toggleGroup} onIgnore={g => addGroupsToIgnore([g])} onIgnoreEpisode={dropMissingEpisode} />)}</div>{visibleCount < visibleGroups.length && <div className="mt-6 flex justify-center"><button onClick={() => setVisibleCount(count => count + 50)} className="btn-outline">加载更多剧集 · 已显示 {visibleCount} / {visibleGroups.length}</button></div>}</> : <div className="empty-state !py-16">
          <span className="mb-5 flex h-16 w-16 items-center justify-center rounded-2xl bg-primary-50 text-primary-400">{query ? <Search size={28} /> : scan?.scannedAt ? <Check size={28} /> : <Library size={28} />}</span>
          <h2 className="text-lg font-semibold">{query ? '没有找到这部剧集' : scan?.scannedAt ? '当前没有待补齐的剧集' : '你的缺集清单，从这里开始'}</h2>
          <p className="mt-2 max-w-sm text-sm leading-7 text-gray-500">{query ? '换个关键词试试，或清除搜索查看全部。' : scan?.scannedAt ? '已匹配剧集暂无缺集，未匹配项目可在扫描诊断中检查。' : '扫描媒体库后，这里会按剧集展示缺失集号和补片入口。'}</p>
          {query ? <button onClick={() => setQuery('')} className="btn-outline mt-6">清除搜索</button> : !scanReady ? <Link to="/settings" className="btn-primary mt-6">配置扫描连接<ArrowRight size={16} /></Link> : !scan?.scannedAt && <button onClick={() => startScan(false)} disabled={busy} className="btn-primary mt-6"><Radar size={16} />开始首次扫描</button>}
        </div>}
      </section>
      {scan?.scannedAt && <ScanDiagnostics scan={scan} />}
      {exemptionsOpen && <Modal title="免检名单" description="只有 TMDB 明确完结且已齐的剧才会自动归档；连载剧即使当前已齐，也会继续检查更新。" onClose={closeExemptionModal}>
        <div className="segmented mb-5" aria-label="免检分类">{[['manual','手动忽略',exemptions.manual.length],['complete','完结归档',exemptions.complete.length],['episode','单集忽略',episodeIgnores.length]].map(([key,label,count]) => <button key={key} aria-pressed={exemptionTab === key} onClick={() => { setExemptionTab(key); setSelectedExemptions({}); }}>{label} · {count}</button>)}</div>
        {selectedExemptionItems.length > 0 && <div className="mb-4 flex items-center justify-between rounded-xl bg-primary-50 p-3 text-sm"><span>已选 {selectedExemptionItems.length} 项</span><button onClick={() => deleteExemptions(selectedExemptionItems)} className="btn-ghost !text-red-600">移出名单</button></div>}
        <div className="space-y-2">{activeExemptions.length ? activeExemptions.map(item => <div key={item.id} className="flex items-center gap-3 rounded-xl border border-gray-200 px-4 py-3"><input type="checkbox" aria-label={`选择 ${item.name}`} checked={!!selectedExemptions[item.id]} onChange={() => toggleExemption(item)} className="h-4 w-4 accent-primary-600" /><div className="min-w-0 flex-1"><p className="truncate text-sm font-medium">{item.name}</p><p className="mt-1 text-xs text-gray-400">{item.episode ? item.tmdbName || '扫描时不再列出这一集' : item.tmdbName || item.tmdbId || '暂无匹配信息'}</p></div><button onClick={() => deleteExemptions([item])} className="btn-ghost !text-red-500">移除</button></div>) : <div className="empty-state !py-10"><ShieldCheck size={25} className="mb-3 text-gray-300" /><p className="text-sm text-gray-500">当前名单为空</p></div>}</div>
      </Modal>}
    </div>
  );
}

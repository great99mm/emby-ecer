import { useEffect, useState } from 'react';
import useStore from '../store';
import { api } from '../api';
import toast from 'react-hot-toast';
import { Radar, Tv, Film, AlertTriangle, Activity, Inbox, RefreshCw, Trash2, CheckSquare, Square, ShieldCheck } from 'lucide-react';
import ProgressBar from '../components/ProgressBar';
import MissingCard from '../components/MissingCard';
import StatCard from '../components/StatCard';

export default function Missing() {
  const missing = useStore(s => s.missing);
  const scan = useStore(s => s.scan);
  const jobStatus = useStore(s => s.jobStatus);
  const setActiveJobId = useStore(s => s.setActiveJobId);
  const setJobStatus = useStore(s => s.setJobStatus);
  const setScan = useStore(s => s.setScan);
  const busy = jobStatus && jobStatus.status !== 'done' && jobStatus.status !== 'error';
  const [selectMode, setSelectMode] = useState(false);
  const [selected, setSelected] = useState({});
  const [exemptions, setExemptions] = useState({ manual: [], complete: [] });
  const [exemptionTab, setExemptionTab] = useState('manual');
  const [selectedExemptions, setSelectedExemptions] = useState({});

  const summary = scan?.summary || {};
  const diagnostics = scan?.diagnostics || {};
  const unmatchedSeries = scan?.unmatched?.series || [];
  const skippedSeries = diagnostics.skipped || [];
  const comparedSeries = diagnostics.compared || [];

  // Group + compute health
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
  const groupList = Object.values(groups);

  // Overall health
  const totalMissing = missing.length;
  const totalTMDB = groupList.reduce((s, g) => s + g.totalEpisodes, 0);
  const totalOwned = groupList.reduce((s, g) => s + g.ownedEpisodes, 0);
  const healthPct = totalTMDB > 0 ? Math.round((totalOwned / totalTMDB) * 100) : 0;

  const startScan = async (recentOnly = false, clearCache = false) => {
    try {
      const data = await api('/api/jobs', { method: 'POST', body: JSON.stringify({ type: 'scan', airedOnly: true, recentOnly, clearCache }) });
      setActiveJobId(data.jobId);
      setJobStatus({ status: 'running', progress: 0, message: '任务已提交...' });
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

  useEffect(() => { loadExemptions(); }, []);

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
      const data = await api('/api/jobs', { method: 'POST', body: JSON.stringify({ type: 'scan', airedOnly: true, seriesIds: ids }) });
      setActiveJobId(data.jobId);
      setJobStatus({ status: 'running', progress: 0, message: `正在单独扫描 ${ids.length} 部剧...` });
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
      const data = await api('/api/exemptions/delete', { method: 'POST', body: JSON.stringify({ ids }) });
      setExemptions({ manual: data.manual || [], complete: data.complete || [] });
      setSelectedExemptions({});
      toast.success('已从免检名单移除');
    } catch (err) {
      toast.error(err.message);
    }
  };

  if (!missing.length) {
    return (
      <div className="space-y-6">
        <ProgressBar />
        <div className="card text-center py-10">
          <Inbox className="w-10 h-10 text-gray-300 mx-auto mb-3" />
          <h2 className="text-lg font-bold text-gray-900">暂无缺失列表</h2>
          <p className="mt-1 text-sm text-gray-500">请先扫描 Emby 媒体库</p>
          <button onClick={() => startScan(false, false)} disabled={busy} className="btn-primary mt-4 mx-auto flex items-center gap-2">
            <Radar className="w-4 h-4" /> 扫描媒体库
          </button>
          <details className="mt-4 rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 text-left">
            <summary className="cursor-pointer list-none text-sm font-bold text-gray-700">高级选项</summary>
            <div className="mt-3 grid grid-cols-1 gap-3">
              <button type="button" onClick={() => startScan(false, true)} disabled={busy} className="btn-outline w-full flex items-center justify-center gap-2 text-red-600 border-red-200 hover:bg-red-50">
                <Trash2 className="w-4 h-4" /> 清空 TMDB 缓存后扫描
              </button>
            </div>
          </details>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <ProgressBar />

      {/* Stats Row */}
      <div className="grid grid-cols-2 xl:grid-cols-6 gap-3">
        <StatCard label="追踪剧集" value={groupList.length} icon={Tv} />
        <StatCard label="实有集数" value={totalOwned} icon={Film} />
        <StatCard label="缺失集数" value={totalMissing} icon={AlertTriangle} accent />
        <StatCard label="重扫剧集" value={summary.seriesRescanned ?? '--'} icon={RefreshCw} />
        <StatCard label="未匹配剧集" value={summary.unmatchedSeries ?? '--'} icon={AlertTriangle} />
        <div className="card flex flex-col justify-between">
          <div className="flex items-center gap-1.5 mb-1">
            <Activity className="w-4 h-4 text-primary-600" />
            <span className="text-xs font-bold text-gray-500">健康度</span>
          </div>
          <div className="text-xl font-extrabold text-gray-900">{healthPct}%</div>
          <div className="mt-2 h-1.5 w-full rounded-full bg-gray-200">
            <div
              className={`h-full rounded-full transition-all ${healthPct >= 80 ? 'bg-emerald-500' : healthPct >= 50 ? 'bg-amber-500' : 'bg-red-500'}`}
              style={{ width: `${healthPct}%` }}
            />
          </div>
        </div>
      </div>

      {/* Scan Button */}
      <button onClick={() => startScan(false, false)} disabled={busy} className="btn-primary w-full flex items-center justify-center gap-2">
        <Radar className="w-4 h-4" /> 全量扫描
      </button>
      <details className="rounded-lg border border-gray-200 bg-gray-50 px-4 py-3">
        <summary className="cursor-pointer list-none text-sm font-bold text-gray-700">高级选项</summary>
        <div className="mt-3 grid grid-cols-1 gap-3">
          <button type="button" onClick={() => startScan(false, true)} disabled={busy} className="btn-outline w-full flex items-center justify-center gap-2 text-red-600 border-red-200 hover:bg-red-50">
            <Trash2 className="w-4 h-4" /> 清空 TMDB 缓存后扫描
          </button>
        </div>
      </details>

      <div className="card space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h2 className="text-base font-bold text-gray-900">免检名单</h2>
            <p className="mt-1 text-xs text-gray-400">手动忽略和完结归档的剧集，扫描时会跳过。</p>
          </div>
          <ShieldCheck className="h-5 w-5 text-emerald-500" />
        </div>
        <div className="flex gap-2">
          <button type="button" onClick={() => { setExemptionTab('manual'); setSelectedExemptions({}); }} className={`rounded-md px-3 py-1.5 text-xs font-bold border ${exemptionTab === 'manual' ? 'bg-primary-600 text-white border-primary-600' : 'bg-white text-gray-600 border-gray-200'}`}>手动忽略 · {exemptions.manual.length}</button>
          <button type="button" onClick={() => { setExemptionTab('complete'); setSelectedExemptions({}); }} className={`rounded-md px-3 py-1.5 text-xs font-bold border ${exemptionTab === 'complete' ? 'bg-primary-600 text-white border-primary-600' : 'bg-white text-gray-600 border-gray-200'}`}>完结归档 · {exemptions.complete.length}</button>
        </div>
        {selectedExemptionItems.length > 0 && (
          <div className="flex items-center justify-between rounded-lg border border-red-100 bg-red-50 px-3 py-2">
            <span className="text-xs font-bold text-red-700">已选 {selectedExemptionItems.length} 项</span>
            <button type="button" onClick={() => deleteExemptions(selectedExemptionItems)} className="text-xs font-bold text-red-600 hover:text-red-700">批量移除</button>
          </div>
        )}
        <div className="max-h-60 overflow-y-auto space-y-2">
          {(exemptionTab === 'manual' ? exemptions.manual : exemptions.complete).length === 0 ? (
            <p className="text-sm text-gray-500 py-3">当前名单为空。</p>
          ) : (
            (exemptionTab === 'manual' ? exemptions.manual : exemptions.complete).map(item => (
              <div key={item.id} className="flex items-center justify-between gap-3 rounded-lg border border-gray-200 bg-gray-50 px-3 py-2">
                <button type="button" onClick={() => toggleExemption(item)} className="shrink-0 text-gray-400 hover:text-primary-600">
                  {selectedExemptions[item.id] ? <CheckSquare className="w-4 h-4" /> : <Square className="w-4 h-4" />}
                </button>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-bold text-gray-900">{item.name}</p>
                  <p className="text-xs text-gray-400">{item.tmdbName || item.tmdbId || '无 TMDB 信息'}</p>
                </div>
                <button type="button" onClick={() => deleteExemptions([item])} className="shrink-0 text-xs font-bold text-red-500 hover:text-red-600">移除</button>
              </div>
            ))
          )}
        </div>
      </div>

      <details className="card space-y-4">
        <summary className="cursor-pointer list-none">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h2 className="text-base font-bold text-gray-900">扫描诊断</h2>
            <p className="mt-1 text-xs text-gray-400">帮助确认哪些剧集命中缓存、真正重扫、未匹配，和为什么被跳过。</p>
          </div>
          <Activity className="h-5 w-5 text-gray-400" />
        </div>
        </summary>
        <div className="mt-4 space-y-4">
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
          <div className="rounded-lg border border-gray-200 bg-gray-50 px-4 py-3">
            <div className="text-xs font-bold text-gray-500">命中缓存</div>
            <div className="mt-1 text-xl font-extrabold text-gray-900">{diagnostics.cacheHits ?? summary.seriesCached ?? 0}</div>
          </div>
          <div className="rounded-lg border border-gray-200 bg-gray-50 px-4 py-3">
            <div className="text-xs font-bold text-gray-500">真正重扫</div>
            <div className="mt-1 text-xl font-extrabold text-gray-900">{diagnostics.rescannedSeries ?? summary.seriesRescanned ?? 0}</div>
          </div>
          <div className="rounded-lg border border-gray-200 bg-gray-50 px-4 py-3">
            <div className="text-xs font-bold text-gray-500">未匹配</div>
            <div className="mt-1 text-xl font-extrabold text-gray-900">{diagnostics.unmatchedSeries ?? summary.unmatchedSeries ?? 0}</div>
          </div>
          <div className="rounded-lg border border-gray-200 bg-gray-50 px-4 py-3">
            <div className="text-xs font-bold text-gray-500">跳过项目</div>
            <div className="mt-1 text-xl font-extrabold text-gray-900">{diagnostics.skippedCount ?? skippedSeries.length ?? 0}</div>
          </div>
        </div>

        <details>
          <summary className="cursor-pointer list-none text-sm font-bold text-gray-700">查看被跳过剧集</summary>
          <div className="mt-3 space-y-2">
            {skippedSeries.length === 0 ? (
              <p className="text-sm text-gray-500">本次没有被跳过的剧集。</p>
            ) : (
              skippedSeries.map((item, i) => (
                <div key={`${item.id || item.name || 'skip'}-${i}`} className="rounded-lg border border-gray-200 bg-gray-50 px-3 py-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-bold text-gray-900">{item.name || '未知剧集'}</p>
                      <p className="mt-1 text-xs text-gray-500">{item.reason || '无原因'}</p>
                    </div>
                    <span className="shrink-0 rounded-full border border-gray-200 bg-white px-2 py-1 text-[11px] font-bold text-gray-500">{item.action || 'skip'}</span>
                  </div>
                </div>
              ))
            )}
          </div>
        </details>

        <details>
          <summary className="cursor-pointer list-none text-sm font-bold text-gray-700">查看未匹配剧集</summary>
          <div className="mt-3 space-y-2">
            {unmatchedSeries.length === 0 ? (
              <p className="text-sm text-gray-500">本次没有未匹配的剧集。</p>
            ) : (
              unmatchedSeries.map((item, i) => (
                <div key={`${item.id || item.name || 'unmatched'}-${i}`} className="rounded-lg border border-red-200 bg-red-50 px-3 py-3">
                  <p className="truncate text-sm font-bold text-red-900">{item.name || '未知剧集'}</p>
                  <p className="mt-1 text-xs text-red-700">{item.reason || '未提供原因'}</p>
                </div>
              ))
            )}
          </div>
        </details>

        <details>
          <summary className="cursor-pointer list-none text-sm font-bold text-gray-700">查看已匹配比对剧集</summary>
          <div className="mt-3 space-y-2">
            {comparedSeries.length === 0 ? (
              <p className="text-sm text-gray-500">暂无已匹配比对明细。</p>
            ) : (
              comparedSeries.map((item, i) => (
                <div key={`${item.id || item.name || 'compared'}-${i}`} className="rounded-lg border border-gray-200 bg-gray-50 px-3 py-3">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-bold text-gray-900">{item.name || '未知剧集'}</p>
                      <p className="mt-1 text-xs text-gray-500">
                        TMDB：{item.tmdbName || item.tmdbId || '未知'}{item.tmdbYear ? ` · ${item.tmdbYear}` : ''}
                      </p>
                      <p className="mt-1 text-xs text-gray-500">{item.reason || '已完成比对'}</p>
                    </div>
                    <div className="shrink-0 text-right text-[11px] font-bold text-gray-500 leading-5">
                      <div>Emby {item.embyEpisodes ?? 0}</div>
                      <div>TMDB {item.tmdbEpisodes ?? 0}</div>
                      <div className={item.missingEpisodes > 0 ? 'text-red-600' : 'text-emerald-600'}>缺 {item.missingEpisodes ?? 0}</div>
                    </div>
                  </div>
                </div>
              ))
            )}
          </div>
        </details>
        </div>
      </details>

      {/* Card Grid */}
      <div>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <p className="text-xs font-bold uppercase tracking-widest text-gray-400">缺集管理 · {groupList.length} 部剧</p>
          <button type="button" onClick={() => { setSelectMode(v => !v); setSelected({}); }} className="btn-outline flex items-center gap-2 px-3 py-1.5 text-xs">
            {selectMode ? <CheckSquare className="w-4 h-4" /> : <Square className="w-4 h-4" />} {selectMode ? '退出多选' : '多选'}
          </button>
        </div>
        {selectMode && (
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-primary-100 bg-primary-50 px-3 py-2">
            <span className="text-xs font-bold text-primary-700">已选 {selectedGroups.length} 部剧</span>
            <div className="flex gap-2">
              <button type="button" onClick={startSelectedScan} disabled={busy || selectedGroups.length === 0} className="rounded-md bg-primary-600 px-3 py-1.5 text-xs font-bold text-white disabled:opacity-40">单独扫描</button>
              <button type="button" onClick={() => addGroupsToIgnore(selectedGroups)} disabled={selectedGroups.length === 0} className="rounded-md border border-red-200 bg-white px-3 py-1.5 text-xs font-bold text-red-600 disabled:opacity-40">加入免检</button>
            </div>
          </div>
        )}
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-3">
          {groupList.map(group => (
            <MissingCard key={group.key} group={group} selectable={selectMode} selected={!!selected[group.embySeriesId]} onToggleSelect={toggleGroup} onIgnore={(g) => addGroupsToIgnore([g])} />
          ))}
        </div>
      </div>
    </div>
  );
}

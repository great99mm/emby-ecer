import { useNavigate } from 'react-router-dom';
import useStore from '../store';
import { api } from '../api';
import toast from 'react-hot-toast';
import { Radar, Clock, AlertTriangle, Tv, Film, ChevronRight, Database, RefreshCw, Trash2 } from 'lucide-react';
import ProgressBar from '../components/ProgressBar';
import StatCard from '../components/StatCard';

export default function Home() {
  const navigate = useNavigate();
  const scan = useStore(s => s.scan);
  const missing = useStore(s => s.missing);
  const jobStatus = useStore(s => s.jobStatus);
  const setActiveJobId = useStore(s => s.setActiveJobId);
  const setJobStatus = useStore(s => s.setJobStatus);

  const summary = scan?.summary || {};
  const diagnostics = scan?.diagnostics || {};
  const unmatchedSeries = scan?.unmatched?.series || [];
  const skippedSeries = diagnostics.skipped || [];
  const scannedAt = scan?.scannedAt;
  const busy = jobStatus && jobStatus.status !== 'done' && jobStatus.status !== 'error';

  const startJob = async (type, recentOnly = false, clearCache = false) => {
    try {
      const data = await api('/api/jobs', { method: 'POST', body: JSON.stringify({ type, airedOnly: true, recentOnly, clearCache }) });
      setActiveJobId(data.jobId);
      setJobStatus({ status: 'running', progress: 0, message: '任务已提交...' });
    } catch (err) {
      toast.error(err.message);
    }
  };

  return (
    <div className="space-y-6">
      {/* Progress Bar */}
      <ProgressBar />

      {/* Stats */}
      <div className="grid grid-cols-2 xl:grid-cols-6 gap-3 sm:gap-4">
        <StatCard
          label="缺集"
          value={summary.totalMissingEpisodes ?? '--'}
          icon={AlertTriangle}
          accent
        />
        <StatCard
          label="剧集"
          value={summary.seriesScanned ?? '--'}
          icon={Tv}
        />
        <StatCard
          label="电影"
          value={summary.movieTotal ?? '--'}
          icon={Film}
        />
        <StatCard
          label="缓存命中"
          value={summary.seriesCached ?? '--'}
          icon={Database}
        />
        <StatCard
          label="重扫剧集"
          value={summary.seriesRescanned ?? '--'}
          icon={RefreshCw}
        />
        <StatCard
          label="未匹配剧集"
          value={summary.unmatchedSeries ?? '--'}
          icon={AlertTriangle}
        />
      </div>

      {/* Scan Actions */}
      <div className="card overflow-hidden p-0">
        <div className="bg-gradient-to-r from-primary-600 via-primary-500 to-blue-500 px-5 py-5 text-white">
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="text-xs font-bold uppercase tracking-[0.18em] text-primary-100">Library Scan</p>
              <h2 className="mt-1 text-2xl font-black tracking-tight">媒体库扫描中心</h2>
              <p className="mt-2 text-sm text-blue-100">扫描 Emby 实际拥有数据，并与 TMDB 官方季集基准做差异比对。</p>
            </div>
            <div className="hidden sm:flex h-12 w-12 shrink-0 items-center justify-center rounded-lg bg-white/15 backdrop-blur">
              <Radar className="w-6 h-6" />
            </div>
          </div>
        </div>
        <div className="px-5 py-4 space-y-4">
          <div className="flex items-center gap-2 text-sm text-gray-500">
            <Clock className="w-4 h-4" />
            <span>上次扫描：{scannedAt ? new Date(scannedAt).toLocaleString('zh-CN') : '尚未扫描'}{summary.scanMode === 'recent' ? ' · 最近变更模式' : summary.scanMode === 'full' ? ' · 全量增量模式' : ''}</span>
          </div>
          <button
            onClick={() => startJob('scan', false, false)}
            disabled={busy}
            className="btn-primary w-full flex items-center justify-center gap-2 text-sm"
          >
            <Radar className="w-4 h-4" />
            全量扫描
          </button>
          <details className="rounded-lg border border-gray-200 bg-gray-50 px-4 py-3">
            <summary className="cursor-pointer list-none text-sm font-bold text-gray-700">高级选项</summary>
            <div className="mt-3 grid grid-cols-1 sm:grid-cols-2 gap-3">
              <button
                type="button"
                onClick={() => startJob('scan', true, false)}
                disabled={busy}
                className="btn-outline w-full flex items-center justify-center gap-2 text-sm"
              >
                <RefreshCw className="w-4 h-4" />
                只扫最近变更
              </button>
              <button
                type="button"
                onClick={() => startJob('scan', false, true)}
                disabled={busy}
                className="btn-outline w-full flex items-center justify-center gap-2 text-sm text-red-600 border-red-200 hover:bg-red-50"
              >
                <Trash2 className="w-4 h-4" />
                清空本地缓存后扫描
              </button>
            </div>
          </details>
        </div>
      </div>

      <div className="card space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h2 className="text-base font-bold text-gray-900">扫描诊断</h2>
            <p className="mt-1 text-xs text-gray-400">查看缓存命中、真正重扫、未匹配数量，以及被跳过的剧集原因。</p>
          </div>
        </div>
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
      </div>

      {/* Recent Missing */}
      <div className="card">
        <details>
          <summary className="cursor-pointer list-none">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0 flex-1">
                <h2 className="text-base font-bold text-gray-900">最近缺失</h2>
                <p className="mt-1 text-xs leading-5 text-gray-400">快速查看最近一次扫描发现的剧集缺口</p>
              </div>
              <div className="flex shrink-0 items-center gap-3 pt-0.5">
                <span className="inline-flex min-w-[56px] items-center justify-center rounded-full border border-red-200 bg-red-50 px-3 py-1 text-xs font-bold text-red-700">
                  {missing.length} 集
                </span>
                <button
                  onClick={(e) => { e.preventDefault(); navigate('/missing'); }}
                  className="inline-flex items-center gap-1 text-sm font-bold text-primary-600 hover:text-primary-700"
                >
                  查看全部
                  <ChevronRight className="w-4 h-4" />
                </button>
              </div>
            </div>
          </summary>
          <div className="mt-4 grid grid-cols-1 sm:grid-cols-2 gap-2">
            {missing.length === 0 ? (
              <p className="text-sm text-gray-500 py-4 text-center sm:col-span-2">还没有扫描结果，点击上方开始扫描</p>
            ) : (
              missing.slice(0, 5).map((item, i) => (
                <div key={i} className="flex items-center justify-between rounded-lg bg-gray-50 px-3 py-3 border border-gray-100">
                  <div className="min-w-0">
                    <p className="truncate text-sm font-bold text-gray-900">{item.officialTitle || item.embyTitle}</p>
                    <p className="text-xs text-gray-500">{item.code} · {item.episodeName || '未命名'}</p>
                  </div>
                  <span className="shrink-0 ml-2 inline-flex items-center px-2 py-0.5 rounded-full text-xs font-bold bg-red-50 text-red-700 border border-red-200">
                    缺失
                  </span>
                </div>
              ))
            )}
          </div>
        </details>
      </div>
    </div>
  );
}

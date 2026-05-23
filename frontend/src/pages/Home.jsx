import { useNavigate } from 'react-router-dom';
import useStore from '../store';
import { api } from '../api';
import toast from 'react-hot-toast';
import { Radar, Clock, AlertTriangle, Tv, Film, ChevronRight, Database, RefreshCw } from 'lucide-react';
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
  const scannedAt = scan?.scannedAt;
  const busy = jobStatus && jobStatus.status !== 'done' && jobStatus.status !== 'error';

  const startJob = async (type, recentOnly = false) => {
    try {
      const data = await api('/api/jobs', { method: 'POST', body: JSON.stringify({ type, airedOnly: true, recentOnly }) });
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
      <div className="grid grid-cols-2 lg:grid-cols-5 gap-3 sm:gap-4">
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
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <button
              onClick={() => startJob('scan', true)}
              disabled={busy}
              className="btn-primary w-full flex items-center justify-center gap-2 text-sm"
            >
              <RefreshCw className="w-4 h-4" />
              只扫最近变更（推荐）
            </button>
            <button
              onClick={() => startJob('scan', false)}
              disabled={busy}
              className="btn-outline w-full flex items-center justify-center gap-2 text-sm"
            >
              <Radar className="w-4 h-4" />
              全量增量扫描
            </button>
          </div>
        </div>
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

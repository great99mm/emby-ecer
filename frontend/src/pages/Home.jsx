import { useNavigate } from 'react-router-dom';
import useStore from '../store';
import { api } from '../api';
import toast from 'react-hot-toast';
import { Radar, Clock, AlertTriangle, Tv, Film, ChevronRight } from 'lucide-react';
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

  const startJob = async (type) => {
    try {
      const data = await api('/api/jobs', { method: 'POST', body: JSON.stringify({ type, airedOnly: true }) });
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
      <div className="grid grid-cols-3 gap-3 sm:gap-4">
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
      </div>

      {/* Scan Actions */}
      <div className="card space-y-3">
        <div className="flex items-center gap-2 text-sm text-gray-500">
          <Clock className="w-4 h-4" />
          <span>上次扫描：{scannedAt ? new Date(scannedAt).toLocaleString('zh-CN') : '尚未扫描'}</span>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-1 gap-2">
          <button
            onClick={() => startJob('scan')}
            disabled={busy}
            className="btn-primary flex items-center justify-center gap-2"
          >
            <Radar className="w-4 h-4" />
            扫描媒体库
          </button>
        </div>
      </div>

      {/* Recent Missing */}
      <div className="card">
        <details>
          <summary className="cursor-pointer list-none">
            <div className="flex items-center justify-between">
              <h2 className="text-base font-bold text-gray-900">最近缺失</h2>
              <div className="flex items-center gap-2">
                <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-bold bg-red-50 text-red-700 border border-red-200">
                  {missing.length} 集
                </span>
                <button
                  onClick={(e) => { e.preventDefault(); navigate('/missing'); }}
                  className="text-xs font-bold text-primary-600 hover:text-primary-700"
                >
                  查看全部 <ChevronRight className="w-3 h-3 inline" />
                </button>
              </div>
            </div>
          </summary>
          <div className="mt-3 space-y-2">
            {missing.length === 0 ? (
              <p className="text-sm text-gray-500 py-4 text-center">还没有扫描结果，点击上方开始扫描</p>
            ) : (
              missing.slice(0, 5).map((item, i) => (
                <div key={i} className="flex items-center justify-between rounded-xl bg-gray-50 px-3 py-2.5">
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

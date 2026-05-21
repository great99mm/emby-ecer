import useStore from '../store';
import { api } from '../api';
import toast from 'react-hot-toast';
import { Radar, Tv, Film, AlertTriangle, Activity, Inbox } from 'lucide-react';
import ProgressBar from '../components/ProgressBar';
import MissingCard from '../components/MissingCard';
import StatCard from '../components/StatCard';

export default function Missing() {
  const missing = useStore(s => s.missing);
  const scan = useStore(s => s.scan);
  const jobStatus = useStore(s => s.jobStatus);
  const setActiveJobId = useStore(s => s.setActiveJobId);
  const setJobStatus = useStore(s => s.setJobStatus);
  const busy = jobStatus && jobStatus.status !== 'done' && jobStatus.status !== 'error';

  const summary = scan?.summary || {};

  // Group + compute health
  const groups = {};
  for (const item of missing) {
    const key = `${item.tmdbId || 0}:${item.officialTitle || item.embyTitle}`;
    if (!groups[key]) {
      groups[key] = {
        key,
        title: item.officialTitle || item.embyTitle,
        tmdbId: item.tmdbId,
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

  const startScan = async () => {
    try {
      const data = await api('/api/jobs', { method: 'POST', body: JSON.stringify({ type: 'scan', airedOnly: true }) });
      setActiveJobId(data.jobId);
      setJobStatus({ status: 'running', progress: 0, message: '任务已提交...' });
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
          <button onClick={startScan} disabled={busy} className="btn-primary mt-4 mx-auto flex items-center gap-2">
            <Radar className="w-4 h-4" /> 扫描媒体库
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <ProgressBar />

      {/* Stats Row */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        <StatCard label="追踪剧集" value={groupList.length} icon={Tv} />
        <StatCard label="实有集数" value={totalOwned} icon={Film} />
        <StatCard label="缺失集数" value={totalMissing} icon={AlertTriangle} accent />
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
      <button onClick={startScan} disabled={busy} className="btn-primary w-full flex items-center justify-center gap-2">
        <Radar className="w-4 h-4" /> 重新扫描
      </button>

      {/* Card Grid */}
      <div>
        <p className="text-xs font-bold uppercase tracking-widest text-gray-400 mb-3">缺集管理 · {groupList.length} 部剧</p>
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-3">
          {groupList.map(group => (
            <MissingCard key={group.key} group={group} />
          ))}
        </div>
      </div>
    </div>
  );
}

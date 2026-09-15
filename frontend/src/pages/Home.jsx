import { useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import useStore from '../store';
import { api } from '../api';
import toast from 'react-hot-toast';
import { Radar, Clock3, ArrowRight, ArrowUpRight, RefreshCw, Server, BadgeCheck, Download, Film, Check, ListChecks } from 'lucide-react';
import ProgressBar from '../components/ProgressBar';
import StatCard from '../components/StatCard';
import ScanDiagnostics from '../components/ScanDiagnostics';
import RadarArt from '../components/RadarArt';
import ConnectionBadge from '../components/ConnectionBadge';

export default function Home() {
  const navigate = useNavigate();
  const { scan, missing, settings, jobStatus, setActiveJobId, setJobStatus, connectionStatus, checkConnections } = useStore();
  const checking = Object.values(connectionStatus).some(item => item.status === 'checking');
  const [starting, setStarting] = useState(false);
  const summary = scan?.summary || {};
  const scannedAt = scan?.scannedAt;
  const ready = settings.ready || {};
  const scanReady = ready.emby && ready.tmdb;
  const busy = starting || (jobStatus && !['done','error'].includes(jobStatus.status));
  const groups = useMemo(() => Object.values(missing.reduce((acc, item) => {
    const key = `${item.tmdbId || 0}:${item.officialTitle || item.embyTitle}`;
    if (!acc[key]) acc[key] = { key, title: item.officialTitle || item.embyTitle, poster: item.posterPath, count: 0, codes: [] };
    acc[key].count++; acc[key].codes.push(item.code); return acc;
  }, {})), [missing]);
  const startScan = async (recentOnly = false) => {
    setStarting(true);
    try {
      const data = await api('/api/jobs', { method: 'POST', body: JSON.stringify({ type: 'scan', airedOnly: true, recentOnly }) });
      setActiveJobId(data.jobId);
      setJobStatus({ id: data.jobId, status: 'running', progress: 0, message: '准备扫描媒体库' });
    } catch (err) { toast.error(err.message); } finally { setStarting(false); }
  };
  const title = busy ? '正在核对你的媒体库' : !scanReady ? '好故事，值得一集不落。' : !scannedAt ? '从一次扫描开始。' : missing.length ? `还有 ${missing.length} 集，等待补齐。` : '本次扫描，暂无缺集。';
  return (
    <div className="page-stack">
      <div className="page-heading">
        <div><p className="eyebrow mb-2">EMBY ECER / OVERVIEW</p><h1 className="page-title">媒体库概览</h1><p className="page-description">发现缺失的剧集，交给 MoviePilot 补齐。</p></div>
        <span className={busy ? 'pill-green' : 'pill'}><span className={`connection-dot ${busy || settings.scanAutoEnabled ? 'is-ready' : ''}`} />{busy ? '扫描进行中' : settings.scanAutoEnabled ? `每 ${settings.scanAutoInterval || 12} 小时自动扫描` : '手动扫描模式'}</span>
      </div>
      <ProgressBar />
      {(scan?.summary?.seriesNeedsReview ?? 0) > 0 && <p className="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-800">{scan.summary.seriesNeedsReview} 部剧与 TMDB 编号不同，已单独检查资源编号。断号结果见下方，TMDB 缺集数仍待确认。<button type="button" className="ml-2 underline" onClick={() => { const section = document.getElementById('scan-diagnostics'); if (section) { section.open = true; section.scrollIntoView({ behavior: 'smooth', block: 'start' }); } }}>查看编号检查</button></p>}
      <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_285px]">
        <section className="hero-panel">
          <div className="hero-copy">
            <span className="inline-flex items-center gap-2 text-xs font-medium text-primary-700"><Radar size={15} />缺集扫描</span>
            <h2 className="hero-title">{title}</h2>
            <p className="max-w-sm text-sm leading-7 text-primary-700/75">{!scanReady ? '连接 Emby 与 TMDB，准确找出已播出但尚未入库的剧集。' : busy ? '扫描在后台继续，你可以随时查看结果。' : missing.length ? `${groups.length} 部剧集存在缺口，匹配资源后即可提交下载。` : '按官方季集信息逐一核对，让补片更有把握。'}</p>
            <div className="mt-6 flex flex-wrap items-center gap-2.5">
              {!scanReady ? <Link to="/settings" className="btn-primary">配置扫描连接<ArrowRight size={16} /></Link> : <button onClick={() => startScan(false)} disabled={busy} className="btn-primary"><Radar size={16} />{busy ? '扫描中…' : '全量扫描'}</button>}
              {scannedAt && <button onClick={() => startScan(true)} disabled={busy || !scanReady} className="btn-outline !border-primary-200 !bg-white/60"><RefreshCw size={15} />增量扫描</button>}
            </div>
            <p className="mt-5 flex items-center gap-1.5 text-xs text-primary-700/65"><Clock3 size={13} />{scannedAt ? `上次扫描 ${new Date(scannedAt).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })}` : '尚未开始首次扫描'}</p>
          </div>
          <RadarArt />
        </section>
        <section className="card !py-5">
          <div className="flex items-center justify-between"><h2 className="section-title">连接状态</h2><div className="flex items-center gap-2"><button type="button" onClick={checkConnections} disabled={checking} className="icon-button !h-7 !w-7" aria-label="重新检测连接" title="重新检测连接"><RefreshCw size={14} className={checking ? 'animate-spin' : ''} /></button><Link to="/settings" className="text-gray-400 hover:text-primary-600" aria-label="管理服务连接"><ArrowUpRight size={17} /></Link></div></div>
          {[[Server,'emby','Emby','媒体库来源'],[BadgeCheck,'tmdb','TMDB','官方季集信息'],[Download,'mp','MoviePilot','资源搜索与下载']].map(([Icon,key,name,desc]) => <div className="connection-row" key={key}><span className="connection-icon"><Icon size={17} /></span><div className="min-w-0 flex-1"><p className="text-sm font-medium">{name}</p><p className="mt-0.5 text-[11px] text-gray-400">{desc}</p>{connectionStatus[key]?.status === 'failed' && <p className="mt-1 break-words text-[11px] leading-4 text-red-500">{connectionStatus[key].error}</p>}</div><ConnectionBadge connection={connectionStatus[key]} /></div>)}
          <p className="mt-3 text-[11px] leading-5 text-gray-400">使用已保存的配置检测，每分钟自动刷新。</p>
        </section>
      </div>
      <div className="metrics-strip">
        <StatCard label="待补齐集数" value={scannedAt ? missing.length : '—'} note="已播出 · 尚未入库" accent={missing.length > 0} />
        <StatCard label="涉及剧集" value={scannedAt ? groups.length : '—'} note="存在缺集的剧集" />
        <StatCard label="已扫描剧集" value={scannedAt ? summary.seriesScanned ?? 0 : '—'} note="本次扫描范围" />
        <StatCard label="待确认剧集" value={scannedAt ? (summary.unmatchedSeries ?? 0) + (summary.seriesNeedsReview ?? 0) : '—'} note="匹配或编号需核对" />
      </div>
      <section>
        <div className="mb-4 flex items-center justify-between gap-3"><div><h2 className="section-title">待补齐的剧集</h2><p className="mt-1 text-xs text-gray-400">从一个缺口开始，让媒体库更完整。</p></div><Link to="/missing" className="btn-ghost !px-0">查看全部<ArrowRight size={16} /></Link></div>
        {groups.length ? <div className="overflow-hidden rounded-2xl border border-gray-200 bg-white divide-y divide-gray-100">{groups.slice(0,5).map(group => <button key={group.key} onClick={() => navigate('/missing?title=' + encodeURIComponent(group.title))} className="flex w-full items-center gap-4 px-5 py-4 text-left transition-colors hover:bg-gray-50"><div className="flex h-14 w-10 shrink-0 items-center justify-center overflow-hidden rounded-md bg-gray-100 text-gray-400">{group.poster ? <img src={'https://image.tmdb.org/t/p/w92' + group.poster} alt="" className="h-full w-full object-cover" /> : <Film size={21} />}</div><div className="min-w-0 flex-1"><p className="truncate text-sm font-semibold">{group.title}</p><p className="mt-1 truncate text-xs text-gray-400">{group.codes.slice(0,4).join(' · ')}{group.codes.length > 4 ? ' …' : ''}</p></div><span className="pill-amber shrink-0">缺 {group.count} 集</span><ArrowUpRight size={16} className="hidden text-gray-400 sm:block" /></button>)}</div> : <div className="empty-state !py-10"><span className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-primary-50 text-primary-500">{scannedAt ? <Check size={24} /> : <ListChecks size={24} />}</span><h3 className="text-sm font-medium">{scannedAt ? '当前没有待补齐的剧集' : '扫描完成后，缺集会出现在这里'}</h3><p className="mt-2 text-xs leading-6 text-gray-400">{scannedAt ? '未匹配的剧集仍可在下方诊断中检查。' : '支持全量扫描、增量扫描和单剧重扫。'}</p></div>}
      </section>
      {scannedAt && <ScanDiagnostics scan={scan} />}
    </div>
  );
}

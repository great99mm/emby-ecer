import { useState } from 'react';
import { Search, RefreshCw, Download, ExternalLink } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import toast from 'react-hot-toast';
import useStore from '../store';
import { api } from '../api';
import Modal from './Modal';

export default function LocalOrderDetails({ item }) {
  const report = item.localOrder;
  const navigate = useNavigate();
  const ready = useStore(s => s.settings.ready?.mp);
  const job = useStore(s => s.jobStatus);
  const setActiveJobId = useStore(s => s.setActiveJobId);
  const setJobStatus = useStore(s => s.setJobStatus);
  const [searchOpen, setSearchOpen] = useState(false);
  const [keyword, setKeyword] = useState(item.name || '');
  const [results, setResults] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [downloads, setDownloads] = useState({});
  const [page, setPage] = useState(1);
  const busy = job && !['done', 'error'].includes(job.status);
  const gaps = (report?.ranges || []).flatMap(range => range.gaps || []);
  const rescan = async () => {
    try {
      const data = await api('/api/jobs', { method: 'POST', body: JSON.stringify({ type: 'scan', airedOnly: true, seriesId: item.id }) });
      setActiveJobId(data.jobId);
      setJobStatus({ status: 'running', progress: 0, message: `正在检查《${item.name}》的资源编号` });
      toast.success('已开始单剧检查');
    } catch (err) { toast.error(err.message); }
  };
  const search = async event => {
    event.preventDefault();
    if (!keyword.trim()) return;
    setLoading(true); setError(''); setResults(null); setPage(1);
    try {
      const data = await api('/api/mp/search', { method: 'POST', body: JSON.stringify({ keyword: keyword.trim(), numberingBasis: 'resource' }) });
      setResults(data.results || []);
      setError((data.errors || []).join('；'));
    } catch (err) { setError(err.message); } finally { setLoading(false); }
  };
  const download = async (result, key) => {
    setDownloads(prev => ({ ...prev, [key]: 'loading' }));
    try {
      await api('/api/mp/download', { method: 'POST', body: JSON.stringify({ rawData: result, tmdbId: '' }) });
      setDownloads(prev => ({ ...prev, [key]: 'done' }));
      toast.success('已提交 MoviePilot 下载');
    } catch (err) { setDownloads(prev => ({ ...prev, [key]: '' })); toast.error(err.message); }
  };
  return <div className="mt-3 rounded-xl border border-primary-100 bg-white p-4">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h4 className="text-sm font-medium">当前资源编号检查</h4>
      <button type="button" onClick={rescan} disabled={busy} className="btn-ghost !min-h-8 !py-1 !text-xs"><RefreshCw size={13} />重新检查</button>
    </div>
    {!report ? <p className="mt-2 text-xs text-gray-500">重新检查后，可查看当前版本的编号断号。</p> : report.issue ? <p className="mt-2 text-xs leading-6 text-amber-700">{report.issue}</p> : <>
      <p className="mt-2 text-sm font-medium text-primary-700">已有 {report.files} 个视频 · {report.gapCount ? `${report.gapCount} 处编号断号` : '检查范围内编号连续'}</p>
      <p className="mt-1 text-xs leading-6 text-gray-500">{report.filenameNumbers} 个视频从文件名读取编号{report.splitFiles > 0 ? `，其中 ${report.splitFiles} 个标注上 / 中 / 下等分段` : ''}。{report.duplicateSlots > 0 ? `重复编号 ${report.duplicateSlots} 个，已去重。` : ''}{report.scrapedNumberConflicts > 0 ? `${report.scrapedNumberConflicts} 个文件编号与刮削编号不同，采用文件编号。` : ''}</p>
      <div className="mt-3 flex flex-wrap gap-2">{report.ranges.map(range => <span key={range.season} className="pill">S{String(range.season).padStart(2, '0')} · E{range.first}–E{range.last} · 已有 {range.owned} 个编号</span>)}</div>
      <p className="mt-3 text-xs leading-6 text-gray-500">只检查上方范围内的断号；开头、结尾及整季是否缺失，需要该资源版本的完整目录确认。分段视频按各自编号计数。此结果不等于 TMDB 故事缺集数。</p>
      {gaps.length > 0 && <details className="mt-3" open><summary className="cursor-pointer text-xs font-medium text-amber-700">查看 {gaps.length} 处断号及前后集</summary><div className="mt-3 max-h-64 space-y-2 overflow-y-auto">{gaps.map(gap => <div key={gap.code} className="rounded-lg bg-amber-50 px-3 py-2"><p className="text-xs font-semibold text-amber-800">{gap.code}</p><p className="mt-1 text-xs leading-5 text-gray-500">前：{gap.before}<br />后：{gap.after}</p></div>)}</div></details>}
    </>}
    <div className="mt-3 flex flex-wrap gap-2"><button type="button" onClick={() => ready ? setSearchOpen(true) : navigate('/settings')} className="btn-outline !min-h-8 !py-1 !text-xs"><Search size={13} />{ready ? 'MP 按片名搜索' : '连接 MoviePilot'}</button><a className="btn-ghost !min-h-8 !py-1 !text-xs" href={`https://www.themoviedb.org/tv/${item.tmdbId}`} target="_blank" rel="noreferrer"><ExternalLink size={13} />查看 TMDB 目录</a></div>
    {searchOpen && <Modal title={`${item.name} · 查找同版本资源`} description="资源编号尚未对应 TMDB。请核对配音、分段和版本；搜索结果不会标为已命中缺集，也不自动按这些编号订阅。" onClose={() => setSearchOpen(false)}>
      <form onSubmit={search} className="flex gap-2"><input className="field flex-1" aria-label="资源版本搜索词" value={keyword} onChange={event => setKeyword(event.target.value)} placeholder="片名、配音或版本名称" /><button className="btn-primary" disabled={loading || !keyword.trim()}><Search size={15} />{loading ? '搜索中…' : '搜索'}</button></form>
      {error && <p role="alert" className="mt-4 text-sm text-red-600">{error}</p>}
      {results && <div className="mt-4 space-y-3"><p className="text-xs text-gray-500">{results.length} 条资源 · 请核对版本后下载</p>{results.slice((page - 1) * 20, page * 20).map((result, index) => {
        const key = result.enclosure || result.title || String(index);
        return <div key={key} className="rounded-xl border border-gray-200 p-3"><p className="break-words text-sm font-medium">{result.title || result.description || '未命名资源'}</p><p className="mt-1 break-words text-xs leading-5 text-gray-500">{result.description}</p><div className="mt-2 flex items-center justify-between gap-2"><span className="text-xs text-gray-400">{result.site_name || result.site || ''}</span><button className="btn-outline !min-h-8 !py-1 !text-xs" disabled={!!downloads[key]} onClick={() => download(result, key)}><Download size={13} />{downloads[key] === 'done' ? '已发送' : downloads[key] === 'loading' ? '提交中' : '下载此资源'}</button></div></div>;
      })}{results.length > 20 && <div className="flex justify-end gap-3 text-xs"><button onClick={() => setPage(p => p - 1)} disabled={page === 1}>上一页</button><span>{page} / {Math.ceil(results.length / 20)}</span><button onClick={() => setPage(p => p + 1)} disabled={page * 20 >= results.length}>下一页</button></div>}</div>}
    </Modal>}
  </div>;
}

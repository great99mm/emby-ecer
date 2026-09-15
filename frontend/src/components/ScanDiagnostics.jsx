import { Activity, ChevronDown } from 'lucide-react';
import LocalOrderDetails from './LocalOrderDetails';

export default function ScanDiagnostics({ scan }) {
  const summary = scan?.summary || {};
  const diagnostics = scan?.diagnostics || {};
  const skipped = diagnostics.skipped || [];
  const unmatched = scan?.unmatched?.series || [];
  const compared = diagnostics.compared || [];
  const review = diagnostics.review || [];
  const lists = [
    ['资源版本与编号', review, '没有编号冲突的剧集。'],
    ['待确认匹配', unmatched, '没有未匹配的剧集。'],
    ['已跳过的剧集', skipped, '本次没有跳过的剧集。'],
    ['已比对的剧集', compared, '暂无比对明细。'],
  ];
  return (
    <details id="scan-diagnostics" className="card group !py-4">
      <summary className="flex list-none items-center gap-3">
        <Activity size={18} className="text-gray-400" />
        <div className="flex-1"><h2 className="text-sm font-medium">扫描诊断</h2><p className="mt-1 text-xs text-gray-400">查看比对结果和跳过原因</p></div>
        <span className="hidden text-xs text-gray-400 sm:block">重扫 {summary.seriesRescanned ?? 0} · 待确认 {(summary.unmatchedSeries ?? 0) + review.length}</span>
        <ChevronDown size={16} className="text-gray-400 transition-transform group-open:rotate-180" />
      </summary>
      <div className="mt-5 grid grid-cols-2 gap-3 border-t border-gray-100 pt-5 sm:grid-cols-4">
        {[
          ['资源版本与编号', review.length],
          ['重新扫描', diagnostics.rescannedSeries ?? summary.seriesRescanned ?? 0],
          ['未匹配', diagnostics.unmatchedSeries ?? summary.unmatchedSeries ?? 0],
          ['已跳过', diagnostics.skippedCount ?? skipped.length],
        ].map(([label,value]) => <div key={label} className="rounded-xl bg-gray-50 px-4 py-3"><p className="text-xs text-gray-500">{label}</p><p className="mt-1 text-lg font-semibold tabular-nums">{value}</p></div>)}
      </div>
      <div className="mt-4 divide-y divide-gray-100">
        {lists.map(([title,items,empty]) => <details key={title} open={title === '资源版本与编号' && items.length > 0} className="py-3"><summary className="flex list-none items-center justify-between text-sm text-gray-600"><span>{title}<span className="ml-2 text-xs text-gray-400">{items.length}</span></span><ChevronDown size={14} /></summary><div className={title === '资源版本与编号' ? 'mt-3 space-y-3' : 'mt-3 max-h-80 space-y-2 overflow-y-auto'}>{items.length ? items.map((item,i) => <div key={item.id || i} className="rounded-xl bg-gray-50 px-4 py-3"><div className="flex items-start justify-between gap-3"><div className="min-w-0"><p className="text-sm font-medium break-words">{item.name || '未知剧集'}</p>{item.tmdbId && <p className="mt-1 text-xs text-gray-500">TMDB · {item.tmdbName || item.tmdbId}{item.tmdbYear ? ` · ${item.tmdbYear}` : ''}</p>}<p className="mt-1 text-xs leading-5 text-gray-500">{item.reason || '已完成比对'}</p></div>{item.embyEpisodes != null && <span className="shrink-0 text-right text-xs leading-6 text-gray-500">已有 {item.embyEpisodes ?? 0} 集<br />TMDB 记录 {item.tmdbEpisodes ?? 0} 集<br /><span className={item.needsReview ? 'text-amber-700' : item.missingEpisodes ? 'text-amber-700' : 'text-primary-600'}>{item.needsReview ? 'TMDB 对应待确认' : `缺 ${item.missingEpisodes ?? 0} 集`}</span></span>}</div>{item.needsReview && <LocalOrderDetails item={item} />}</div>) : <p className="py-2 text-xs text-gray-400">{empty}</p>}</div></details>)}
      </div>
    </details>
  );
}

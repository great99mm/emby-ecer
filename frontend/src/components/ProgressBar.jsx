import useStore from '../store';
import { CheckCircle2, AlertCircle, Loader2, X } from 'lucide-react';

export default function ProgressBar() {
  const jobStatus = useStore(s => s.jobStatus);
  const clearJob = useStore(s => s.clearJob);
  if (!jobStatus) return null;
  const { status, progress = 0, message, current, error } = jobStatus;
  const isDone = status === 'done';
  const isError = status === 'error';
  const isFinal = isDone || isError;
  const value = Math.max(0, Math.min(100, progress));
  const Icon = isDone ? CheckCircle2 : isError ? AlertCircle : Loader2;
  return (
    <section className={`rounded-2xl border p-5 ${isError ? 'border-red-200 bg-red-50' : 'border-primary-200 bg-white'}`} aria-label="扫描进度">
      <div className="flex items-center gap-3"><Icon size={20} className={`${isError ? 'text-red-500' : 'text-primary-500'} ${!isFinal ? 'animate-spin' : ''}`} /><div className="min-w-0 flex-1"><p className="text-sm font-medium" role="status">{message || '正在准备扫描…'}</p>{current && !isFinal && <p className="mt-1 break-words text-xs text-gray-500">{current}</p>}</div><span className="text-xs tabular-nums text-gray-500">{value}%</span>{isFinal && <button onClick={clearJob} className="icon-button !h-8 !w-8" aria-label="关闭扫描进度"><X size={17} /></button>}</div>
      {!isError && <div className="mt-4 h-1.5 overflow-hidden rounded-full bg-gray-100" role="progressbar" aria-label="扫描完成比例" aria-valuemin={0} aria-valuemax={100} aria-valuenow={value}><div className="h-full rounded-full bg-primary-500 transition-all duration-500" style={{width: `${value}%`}} /></div>}
      {isError && error && <p className="mt-3 break-words text-xs leading-6 text-red-600">{error}</p>}
    </section>
  );
}

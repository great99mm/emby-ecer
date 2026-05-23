import useStore from '../store';

export default function ProgressBar() {
  const jobStatus = useStore(s => s.jobStatus);
  const clearJob = useStore(s => s.clearJob);

  if (!jobStatus) return null;

  const { status, progress = 0, message = '', current = '' } = jobStatus;
  const isDone = status === 'done';
  const isError = status === 'error';
  const isFinal = isDone || isError;

  return (
    <div className={`rounded-xl border p-4 shadow-sm ${
      isError ? 'border-red-200 bg-red-50' : isDone ? 'border-emerald-200 bg-emerald-50' : 'border-primary-200 bg-white'
    }`}>
      <div className="flex items-center justify-between mb-2">
        <span className="text-sm font-bold text-gray-800">{message || '处理中...'}</span>
        <span className="text-xs font-bold text-gray-500">{progress}%</span>
      </div>
      <div className="h-2.5 w-full overflow-hidden rounded-full bg-gray-200">
        <div
          className={`h-full rounded-full transition-all duration-500 ${
            isError ? 'bg-red-500' : isDone ? 'bg-emerald-500' : 'bg-primary-500'
          }`}
          style={{ width: `${progress}%` }}
        />
      </div>
      {!!current && !isFinal && (
        <div className="mt-3 rounded-lg border border-gray-200 bg-gray-50 px-3 py-2">
          <div className="text-[11px] font-bold uppercase tracking-wider text-gray-400">当前扫描</div>
          <div className="mt-1 text-sm font-medium text-gray-700 break-all">{current}</div>
        </div>
      )}
      {isFinal && (
        <button onClick={clearJob} className="mt-3 w-full rounded-md py-2 text-xs font-bold border border-gray-300 hover:bg-gray-100 transition-colors">
          关闭
        </button>
      )}
    </div>
  );
}

import useStore from '../store';

export default function ProgressBar() {
  const jobStatus = useStore(s => s.jobStatus);
  const clearJob = useStore(s => s.clearJob);

  if (!jobStatus) return null;

  const { status, progress = 0, message = '' } = jobStatus;
  const isDone = status === 'done';
  const isError = status === 'error';
  const isFinal = isDone || isError;

  return (
    <div className={`rounded-2xl border-2 p-3 shadow-sm ${
      isError ? 'border-red-300 bg-red-50' : isDone ? 'border-emerald-300 bg-emerald-50' : 'border-primary-300 bg-white'
    }`}>
      <div className="flex items-center justify-between mb-1.5">
        <span className="text-xs font-bold text-gray-700">{message || '处理中...'}</span>
        <span className="text-xs font-bold text-gray-500">{progress}%</span>
      </div>
      <div className="h-2 w-full overflow-hidden rounded-full bg-gray-200">
        <div
          className={`h-full rounded-full transition-all duration-500 ${
            isError ? 'bg-red-500' : isDone ? 'bg-emerald-500' : 'bg-primary-500'
          }`}
          style={{ width: `${progress}%` }}
        />
      </div>
      {isFinal && (
        <button onClick={clearJob} className="mt-2 w-full rounded-xl py-1.5 text-xs font-bold border border-gray-300 hover:bg-gray-100 transition-colors">
          关闭
        </button>
      )}
    </div>
  );
}

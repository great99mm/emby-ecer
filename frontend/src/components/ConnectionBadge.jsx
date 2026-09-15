const labels = {
  connected: '连接正常',
  failed: '连接失败',
  checking: '检测中',
  unconfigured: '未配置',
  idle: '待检测',
};

export default function ConnectionBadge({ connection, compact = false }) {
  const status = connection?.status || 'idle';
  const label = labels[status] || labels.idle;
  const detail = status === 'failed' ? connection.error : connection?.checkedAt
    ? `最近检测 ${new Date(connection.checkedAt).toLocaleTimeString('zh-CN')}${status === 'connected' && connection.latencyMs != null ? ` · ${connection.latencyMs} ms` : ''}`
    : label;
  const tone = status === 'failed' ? '!bg-red-50 !text-red-600' : status === 'connected' ? 'pill-green' : 'pill';
  return (
    <span title={detail} className={compact ? `flex shrink-0 items-center gap-1.5 ${status === 'failed' ? 'text-red-500' : 'text-gray-500'}` : `pill shrink-0 !text-[10px] ${tone}`}>
      <i className={`connection-dot ${status === 'connected' ? 'is-ready' : status === 'failed' ? '!bg-red-400' : status === 'checking' ? '!bg-primary-400 animate-pulse' : ''}`} />
      {label}
    </span>
  );
}

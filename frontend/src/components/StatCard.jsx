export default function StatCard({ label, value, note, accent }) {
  return <div className={`metric ${accent ? 'accent' : ''}`}><div className="metric-label">{label}</div><div className="metric-value">{value}</div>{note && <div className="metric-note">{note}</div>}</div>;
}

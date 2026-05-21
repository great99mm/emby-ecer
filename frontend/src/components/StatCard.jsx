export default function StatCard({ label, value, icon: Icon, accent }) {
  return (
    <div className={`rounded-2xl p-4 border shadow-sm ${accent ? 'bg-red-50 border-red-200' : 'bg-white border-gray-200'}`}>
      {Icon && <Icon className={`w-5 h-5 mb-2 ${accent ? 'text-red-600' : 'text-primary-600'}`} />}
      <div className="text-2xl font-extrabold tracking-tight text-gray-900">{value}</div>
      <div className="text-xs font-bold text-gray-500 mt-0.5">{label}</div>
    </div>
  );
}

export default function StatCard({ label, value, icon: Icon, accent }) {
  return (
    <div className={`rounded-xl p-4 border shadow-sm ${accent ? 'bg-red-50 border-red-200' : 'bg-white border-gray-200'}`}>
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-[11px] font-bold uppercase tracking-[0.18em] text-gray-400">{label}</div>
          <div className="mt-1 text-3xl font-black tracking-tight text-gray-900">{value}</div>
        </div>
        {Icon && (
          <div className={`flex h-11 w-11 items-center justify-center rounded-lg ${accent ? 'bg-red-100 text-red-600' : 'bg-primary-50 text-primary-600'}`}>
            <Icon className="w-5 h-5" />
          </div>
        )}
      </div>
    </div>
  );
}

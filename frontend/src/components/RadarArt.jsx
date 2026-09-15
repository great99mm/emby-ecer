export default function RadarArt() {
  return (
    <svg viewBox="0 0 220 220" className="radar-art" fill="none" aria-hidden="true">
      <circle cx="110" cy="110" r="90" stroke="currentColor" strokeOpacity=".24" />
      <circle cx="110" cy="110" r="64" stroke="currentColor" strokeOpacity=".35" />
      <circle cx="110" cy="110" r="37" stroke="currentColor" strokeOpacity=".45" />
      <path d="M20 110h180M110 20v180" stroke="currentColor" strokeOpacity=".18" />
      <path d="M110 110L173.6 46.4A90 90 0 0 1 199 97Z" fill="currentColor" fillOpacity=".09" />
      <path d="M110 110l63.6-63.6" stroke="currentColor" strokeOpacity=".65" />
      <circle cx="110" cy="110" r="5" fill="currentColor" />
      <circle cx="154" cy="74" r="7" fill="#3b82f6" stroke="#eff6ff" strokeWidth="4" />
      <circle cx="70" cy="133" r="4" fill="#93c5fd" />
      <rect x="133" y="132" width="39" height="47" rx="8" fill="#fff" stroke="#bfdbfe" />
      <path d="m144 154 6 6 12-14" stroke="#2563eb" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

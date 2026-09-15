import { useEffect, useId, useRef } from 'react';
import { X } from 'lucide-react';

export default function Modal({ title, description, onClose, children }) {
  const ref = useRef(null);
  const titleId = useId();
  useEffect(() => {
    const dialog = ref.current;
    const focused = document.activeElement;
    const overflow = document.body.style.overflow;
    dialog.showModal();
    document.body.style.overflow = 'hidden';
    return () => {
      dialog.close();
      document.body.style.overflow = overflow;
      focused?.focus?.();
    };
  }, []);
  return (
    <dialog ref={ref} className="dialog" aria-labelledby={titleId} onCancel={e => { e.preventDefault(); onClose(); }} onClick={e => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="dialog-header">
        <div className="min-w-0">
          <h2 id={titleId} className="section-title break-words">{title}</h2>
          {description && <p className="mt-1 text-xs text-gray-500">{description}</p>}
        </div>
        <button type="button" onClick={onClose} className="icon-button" aria-label="关闭弹窗"><X size={19} /></button>
      </div>
      <div className="dialog-body">{children}</div>
    </dialog>
  );
}

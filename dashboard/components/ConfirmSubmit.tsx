'use client';

export default function ConfirmSubmit({ message, className, children }: { message: string; className?: string; children: React.ReactNode }) {
  return (
    <button type="submit" className={className} onClick={(e) => !window.confirm(message) && e.preventDefault()}>
      {children}
    </button>
  );
}

import { useId, type ReactNode } from "react";

interface TooltipProps {
  label: string;
  children: ReactNode;
}

/** A focusable "?" that reveals an explanation on hover or focus. The text is linked with aria-describedby. */
export function Tooltip({ label, children }: TooltipProps) {
  const id = useId();
  return (
    <span className="tip">
      <button
        type="button"
        className="tip-trigger"
        aria-describedby={id}
        aria-label={`About: ${label}`}
      >
        ?
      </button>
      <span role="tooltip" id={id} className="tip-body">
        {children}
      </span>
    </span>
  );
}

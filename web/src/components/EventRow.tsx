import { useState } from "react";
import type { TimelineEvent } from "../lib/timeline";
import { eventStyle } from "../lib/eventStyle";
import { summaryLine } from "../lib/eventSummary";
import { prettyJSON, truncate } from "../lib/json";
import { formatMs } from "../lib/status";

/** Characters of pretty JSON shown before "Show more". */
export const PREVIEW_CHARS = 1200;

interface EventRowProps {
  item: TimelineEvent;
}

export function EventRow({ item }: EventRowProps) {
  const [open, setOpen] = useState(false);
  const [showAll, setShowAll] = useState(false);
  const [copied, setCopied] = useState(false);

  const { event, offsetMs, retry } = item;
  const style = eventStyle(event.type, retry !== null);
  const line = summaryLine(event, retry);
  const body = truncate(prettyJSON(event.payload), showAll ? Infinity : PREVIEW_CHARS);

  const copy = () => {
    const clipboard = navigator.clipboard as Clipboard | undefined;
    if (clipboard === undefined) return;
    void clipboard.writeText(prettyJSON(event.payload)).then(() => {
      setCopied(true);
      setTimeout(() => {
        setCopied(false);
      }, 1500);
    });
  };

  return (
    <li className={`event event-${style.tone}${retry !== null ? " event-retry" : ""}`}>
      <button
        type="button"
        className="event-head"
        aria-expanded={open}
        onClick={() => {
          setOpen((v) => !v);
        }}
      >
        <span className="event-offset">+{formatMs(offsetMs)}</span>
        <span className="event-glyph" aria-hidden="true">
          {style.glyph}
        </span>
        <span className="event-label">{style.label}</span>
        <span className="event-summary">{line}</span>
        <span className="event-seq">#{event.seq}</span>
      </button>
      {open && (
        <div className="event-body">
          <div className="event-tools">
            <button type="button" className="button button-small" onClick={copy}>
              {copied ? "Copied" : "Copy JSON"}
            </button>
          </div>
          <pre>{body.text}</pre>
          {body.truncated && !showAll && (
            <button
              type="button"
              className="button button-small"
              onClick={() => {
                setShowAll(true);
              }}
            >
              Show more ({String(body.totalChars - PREVIEW_CHARS)} more characters)
            </button>
          )}
        </div>
      )}
    </li>
  );
}

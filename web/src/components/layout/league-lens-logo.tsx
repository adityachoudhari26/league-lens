import { cn } from "cn"

/** Lifts the tile in dark mode so it doesn't sink into the dark sidebar. */
export function LeagueLensLogo({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 512 512"
      aria-hidden="true"
      className={cn(
        "[--logo-tile:#191b20] dark:[--logo-tile:#2c3037]",
        className,
      )}
    >
      <rect
        x="0"
        y="0"
        width="512"
        height="512"
        rx="114"
        fill="var(--logo-tile)"
      />
      <g transform="rotate(-25 240 240)">
        <path
          d="M35 240 A230.6 230.6 0 0 1 445 240 A230.6 230.6 0 0 1 35 240Z"
          fill="#e8eaed"
          stroke="#e8eaed"
          strokeWidth="10"
          strokeLinejoin="round"
        />
        <path
          d="M107 171 Q97 240 107 309"
          fill="none"
          stroke="var(--logo-tile)"
          strokeWidth="14"
          strokeLinecap="round"
        />
        <path
          d="M373 171 Q383 240 373 309"
          fill="none"
          stroke="var(--logo-tile)"
          strokeWidth="14"
          strokeLinecap="round"
        />
      </g>
      <line
        x1="320"
        y1="320"
        x2="410"
        y2="410"
        stroke="var(--logo-tile)"
        strokeWidth="84"
        strokeLinecap="round"
      />
      <line
        x1="326"
        y1="326"
        x2="406"
        y2="406"
        stroke="#86d9b3"
        strokeWidth="46"
        strokeLinecap="round"
      />
      <circle
        cx="240"
        cy="240"
        r="86"
        fill="var(--logo-tile)"
        stroke="var(--logo-tile)"
        strokeWidth="40"
      />
      <circle
        cx="240"
        cy="240"
        r="86"
        fill="var(--logo-tile)"
        stroke="#86d9b3"
        strokeWidth="28"
      />
      <g transform="rotate(-25 240 240)">
        <line
          x1="197"
          y1="240"
          x2="283"
          y2="240"
          stroke="#e8eaed"
          strokeWidth="15"
          strokeLinecap="round"
        />
        <line
          x1="211.3"
          y1="221"
          x2="211.3"
          y2="259"
          stroke="#e8eaed"
          strokeWidth="15"
          strokeLinecap="round"
        />
        <line
          x1="240"
          y1="221"
          x2="240"
          y2="259"
          stroke="#e8eaed"
          strokeWidth="15"
          strokeLinecap="round"
        />
        <line
          x1="268.7"
          y1="221"
          x2="268.7"
          y2="259"
          stroke="#e8eaed"
          strokeWidth="15"
          strokeLinecap="round"
        />
      </g>
      <circle cx="200" cy="200" r="12" fill="#e8eaed" />
    </svg>
  )
}

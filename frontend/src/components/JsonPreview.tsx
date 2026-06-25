interface JsonPreviewProps {
  payload: unknown;
  emptyText?: string;
}

export function JsonPreview({ payload, emptyText = 'Upload a session to preview JSON.' }: JsonPreviewProps) {
  return (
    <pre className="max-h-[34rem] overflow-auto rounded-md border border-rack-steel/30 bg-rack-soot/80 p-4 text-xs leading-relaxed text-amberline-100">
      {payload ? JSON.stringify(payload, null, 2) : emptyText}
    </pre>
  );
}

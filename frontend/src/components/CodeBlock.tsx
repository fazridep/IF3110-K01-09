export function CodeBlock({ code, label = "Kode" }: { code: string; label?: string }) {
  const lines = code.replace(/\n$/, "").split("\n");
  return (
    <pre className="code" tabIndex={0} aria-label={label}>
      <code>
        {lines.map((line, i) => (
          <span key={i} className="line">
            {line || " "}
          </span>
        ))}
      </code>
    </pre>
  );
}
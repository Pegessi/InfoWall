import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import type { Components } from "react-markdown";

interface MarkdownProps {
  children: string;
}

export function Markdown({ children }: MarkdownProps) {
  const components: Components = {
    a: ({ href, children: linkChildren, ...props }) => (
      <a href={href} target="_blank" rel="noreferrer noopener" {...props}>
        {linkChildren}
      </a>
    ),
    img: ({ src, alt, ...props }) => (
      <img
        src={src}
        alt={alt || ""}
        loading="lazy"
        className="rounded-lg"
        {...props}
      />
    ),
    code: ({ inline, className, children, ...props }: any) => {
      if (inline) {
        return (
          <code
            className="bg-[hsl(var(--muted))] rounded px-1.5 py-0.5 text-[0.85em] font-mono"
            {...props}
          >
            {children}
          </code>
        );
      }
      return (
        <code className={className} {...props}>
          {children}
        </code>
      );
    },
    pre: ({ children, ...props }: any) => (
      <pre
        className="rounded-lg border border-[hsl(var(--border))] bg-[hsl(var(--muted))] p-4 overflow-x-auto font-mono text-[0.85em]"
        {...props}
      >
        {children}
      </pre>
    ),
  };

  return (
    <div className="prose-infowall max-w-none">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={components}
      >
        {children}
      </ReactMarkdown>
    </div>
  );
}

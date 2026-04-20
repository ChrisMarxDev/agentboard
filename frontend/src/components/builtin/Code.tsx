import { Highlight, themes } from 'prism-react-renderer'
import { useData } from '../../hooks/useData'
import { useResolvedTheme } from '../../hooks/useResolvedTheme'

interface CodeProps {
  source: string
  language?: string
}

export function Code({ source, language }: CodeProps) {
  const { data, loading } = useData(source)
  const resolved = useResolvedTheme()
  if (loading) return null

  let code: string
  let lang = language ?? 'text'

  if (typeof data === 'string') {
    code = data
  } else if (data && typeof data === 'object' && !Array.isArray(data)) {
    const obj = data as Record<string, unknown>
    code = String(obj.code ?? obj.source ?? '')
    if (!language && typeof obj.language === 'string') lang = obj.language
  } else {
    code = JSON.stringify(data, null, 2)
    if (!language) lang = 'json'
  }

  if (!code) return null

  const theme = resolved === 'dark' ? themes.vsDark : themes.vsLight

  return (
    <Highlight code={code} language={lang} theme={theme}>
      {({ className, style, tokens, getLineProps, getTokenProps }) => (
        <pre
          className={className}
          style={{
            ...style,
            margin: '1rem 0',
            padding: '1rem',
            borderRadius: '0.5rem',
            fontSize: '0.8rem',
            lineHeight: 1.5,
            overflowX: 'auto',
            border: '1px solid var(--border)',
          }}
        >
          {tokens.map((line, i) => (
            <div key={i} {...getLineProps({ line })}>
              {line.map((token, ti) => (
                <span key={ti} {...getTokenProps({ token })} />
              ))}
            </div>
          ))}
        </pre>
      )}
    </Highlight>
  )
}

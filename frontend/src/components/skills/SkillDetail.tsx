import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, Copy, Download, FileText } from 'lucide-react'
import { useSkill, type SupportingFile } from '../../hooks/useSkill'
import SkillBody from './SkillBody'

/**
 * Route component for `/skills/:slug`. Fetches the skill via the hook, renders
 * the SKILL.md body (frontmatter stripped) + a list of supporting files +
 * actions to copy the shareable URL or download the zip bundle.
 */
export default function SkillDetail() {
  const { slug = '' } = useParams()
  const { skill, loading, error } = useSkill(slug)
  const [copied, setCopied] = useState(false)

  const absoluteBundleUrl = () => {
    if (!skill) return ''
    if (typeof window === 'undefined') return skill.bundleUrl
    return new URL(skill.bundleUrl, window.location.origin).toString()
  }

  const copyUrl = async () => {
    try {
      await navigator.clipboard.writeText(absoluteBundleUrl())
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      // clipboard can fail on non-HTTPS or missing permission; surface via label
      setCopied(false)
    }
  }

  if (loading && !skill) {
    return (
      <div style={{ color: 'var(--text-secondary)' }} className="text-sm">
        Loading…
      </div>
    )
  }

  if (error || !skill) {
    return (
      <div className="flex flex-col gap-4">
        <Link
          to="/skills"
          className="inline-flex items-center gap-1 text-sm self-start"
          style={{ color: 'var(--text-secondary)' }}
        >
          <ArrowLeft size={14} /> All skills
        </Link>
        <div
          className="p-4 rounded-md text-sm"
          style={{ background: 'rgba(239,68,68,0.08)', color: 'var(--error)' }}
        >
          {error || 'Skill not found'}
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      <Link
        to="/skills"
        className="inline-flex items-center gap-1 text-sm self-start"
        style={{ color: 'var(--text-secondary)' }}
      >
        <ArrowLeft size={14} /> All skills
      </Link>

      <header>
        <div className="flex items-baseline gap-2">
          <h1 className="text-2xl font-semibold" style={{ color: 'var(--text)' }}>
            {skill.name}
          </h1>
          <span className="text-xs" style={{ color: 'var(--text-secondary)' }}>
            ({skill.slug})
          </span>
        </div>
        <p className="text-sm mt-1" style={{ color: 'var(--text-secondary)' }}>
          {skill.description}
        </p>
      </header>

      <div className="flex items-center gap-2 flex-wrap">
        <button
          onClick={copyUrl}
          aria-label="Copy shareable URL"
          className="inline-flex items-center gap-1.5 text-sm px-3 h-8 rounded-md"
          style={{
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border)',
            color: 'var(--text)',
            cursor: 'pointer',
          }}
        >
          <Copy size={14} />
          {copied ? 'Copied URL' : 'Copy URL'}
        </button>
        <a
          href={skill.bundleUrl}
          className="inline-flex items-center gap-1.5 text-sm px-3 h-8 rounded-md"
          style={{
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border)',
            color: 'var(--text)',
          }}
        >
          <Download size={14} />
          Download bundle
        </a>
      </div>

      <section>
        <h2
          className="text-[11px] uppercase tracking-wide mb-3"
          style={{ color: 'var(--text-secondary)' }}
        >
          SKILL.md
        </h2>
        <div
          className="p-4 rounded-md"
          style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)' }}
        >
          <SkillBody source={skill.manifestBody} />
        </div>
      </section>

      {skill.files.length > 0 && (
        <section>
          <h2
            className="text-[11px] uppercase tracking-wide mb-3"
            style={{ color: 'var(--text-secondary)' }}
          >
            Supporting files
          </h2>
          <ul className="flex flex-col gap-1">
            {skill.files.map(f => (
              <SupportingFileRow key={f.name} file={f} />
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}

function SupportingFileRow({ file }: { file: SupportingFile }) {
  return (
    <li
      className="flex items-center justify-between gap-3 px-3 py-2 rounded-md"
      style={{
        background: 'var(--bg-secondary)',
        border: '1px solid var(--border)',
      }}
    >
      <a
        href={file.url}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex items-center gap-2 min-w-0 flex-1"
        style={{ color: 'var(--text)' }}
      >
        <FileText size={14} style={{ color: 'var(--text-secondary)' }} />
        <span className="truncate text-sm">{file.name}</span>
      </a>
      <span
        className="text-xs shrink-0"
        style={{ color: 'var(--text-secondary)' }}
      >
        {formatBytes(file.size)}
      </span>
    </li>
  )
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

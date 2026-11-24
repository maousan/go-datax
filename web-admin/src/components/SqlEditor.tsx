import Editor from '@monaco-editor/react'
import { useEffect } from 'react'

interface SqlEditorProps {
  value: string
  onChange: (value: string) => void
  height?: string
  theme?: 'light' | 'dark'
}

export default function SqlEditor({
  value,
  onChange,
  height = '200px',
  theme = 'light',
}: SqlEditorProps) {
  useEffect(() => {
    void import('../monaco')
  }, [])

  return (
    <div
      style={{
        height: height === '100%' ? '100%' : undefined,
        border: '1px solid var(--color-border-2)',
        borderRadius: 6,
        overflow: 'hidden',
        boxSizing: height === '100%' ? 'border-box' : undefined,
      }}
    >
      <Editor
        height={height}
        language="sql"
        theme={theme === 'dark' ? 'vs-dark' : 'vs'}
        value={value}
        onChange={(next) => onChange(next ?? '')}
        options={{
          automaticLayout: true,
          fontSize: 13,
          minimap: { enabled: false },
          scrollBeyondLastLine: false,
          tabSize: 2,
          wordWrap: 'on',
        }}
      />
    </div>
  )
}

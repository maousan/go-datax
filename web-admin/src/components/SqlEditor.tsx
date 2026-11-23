import Editor from '@monaco-editor/react'
import { useEffect } from 'react'

interface SqlEditorProps {
  value: string
  onChange: (value: string) => void
  height?: string
}

export default function SqlEditor({ value, onChange, height = '200px' }: SqlEditorProps) {
  useEffect(() => {
    void import('../monaco')
  }, [])

  return (
    <div style={{ border: '1px solid var(--color-border-2)', borderRadius: 6, overflow: 'hidden' }}>
      <Editor
        height={height}
        language="sql"
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

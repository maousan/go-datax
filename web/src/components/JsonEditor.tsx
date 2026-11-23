import Editor from '@monaco-editor/react'
import { useEffect } from 'react'

interface JsonEditorProps {
  value: string
  onChange: (value: string) => void
  height?: string
}

export default function JsonEditor({ value, onChange, height = '360px' }: JsonEditorProps) {
  useEffect(() => {
    // 只在实际渲染编辑器时加载 Monaco 与 worker 配置。
    void import('../monaco')
  }, [])

  return (
    <div style={{ border: '1px solid #d9d9d9', borderRadius: 6, overflow: 'hidden' }}>
      <Editor
        height={height}
        language="json"
        value={value}
        onChange={(next) => onChange(next ?? '')}
        options={{
          automaticLayout: true,
          fontSize: 13,
          minimap: { enabled: false },
          scrollBeyondLastLine: false,
          tabSize: 2,
        }}
      />
    </div>
  )
}

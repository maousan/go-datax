// Package web 内嵌管理台前端构建产物。
package web

import "embed"

// Dist 包含 web/dist 下的全部文件；未构建前端时至少包含占位文件。
//
//go:embed all:dist
var Dist embed.FS

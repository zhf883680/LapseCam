package web

import _ "embed"

// IndexHTML 是内置的网页管理后台（单文件，无构建依赖）。
//go:embed index.html
var IndexHTML string

package store

import "errors"

// 工作台与笔记操作中可被 errors.Is 判断的哨兵错误。
var (
	// ErrNotWorkspace 表示目标目录不是跬步工作台（缺少 .kuibu/config.json）。
	ErrNotWorkspace = errors.New("不是跬步工作台")

	// ErrAlreadyInit 表示目录已经是工作台，不能重复初始化。
	ErrAlreadyInit = errors.New("目录已经是跬步工作台")

	// ErrNotFound 表示笔记或父笔记不存在。
	ErrNotFound = errors.New("笔记不存在")

	// ErrCycle 表示移动笔记会导致父子关系成环。
	ErrCycle = errors.New("移动会导致父子关系成环")

	// ErrHasChildren 表示笔记仍有子笔记，且未要求递归删除。
	ErrHasChildren = errors.New("笔记仍有子笔记")

	// ErrInvalid 表示工作台或笔记数据不合法。
	ErrInvalid = errors.New("数据不合法")
)

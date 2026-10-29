package store

import (
	"errors"
	"fmt"
)

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

	// ErrConflict 表示姓名/别名与在职人员冲突。
	ErrConflict = errors.New("姓名或别名冲突")

	// ErrAmbiguousPerson 表示按姓名/别名匹配到多个人员。
	ErrAmbiguousPerson = errors.New("姓名匹配不唯一")

	// ErrNeedsConfirm 表示操作有连带影响，需要人工确认（CLI 退出码 8）。
	ErrNeedsConfirm = errors.New("需要确认")

	// ErrDuplicate 表示待办 external_id 已被另一条待办占用。
	ErrDuplicate = errors.New("external_id 已存在")
)

// todoError 是待办操作专用的私有错误类型：Error() 返回待办文案，Unwrap()
// 返回哨兵，保证 errors.Is(err, ErrNotFound/ErrHasChildren/ErrCycle) 仍然成立；
// ErrInvalid 继续直接用 fmt.Errorf("%w", ...) 包装，不受影响。
type todoError struct {
	msg string
	err error
}

// Error 实现 error 接口。
func (e *todoError) Error() string { return e.msg }

// Unwrap 返回底层哨兵错误。
func (e *todoError) Unwrap() error { return e.err }

// newTodoError 构造带自定义文案、但保持哨兵可识别的待办错误。
func newTodoError(sentinel error, format string, args ...any) error {
	return &todoError{msg: fmt.Sprintf(format, args...), err: sentinel}
}

// personError 是组员操作专用的私有错误类型：Error() 返回组员文案，Unwrap()
// 返回哨兵，保证 errors.Is(err, ErrNotFound/ErrConflict/ErrAmbiguousPerson) 成立。
type personError struct {
	msg string
	err error
}

// Error 实现 error 接口。
func (e *personError) Error() string { return e.msg }

// Unwrap 返回底层哨兵错误。
func (e *personError) Unwrap() error { return e.err }

// newPersonError 构造带自定义文案、但保持哨兵可识别的组员错误。
func newPersonError(sentinel error, format string, args ...any) error {
	return &personError{msg: fmt.Sprintf(format, args...), err: sentinel}
}

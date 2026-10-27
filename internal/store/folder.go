package store

import (
	"fmt"
	"strings"
	"time"
)

// FolderType 是目录节点的笔记类型。
//
// 笔记树里只有目录能包含子节点，其余类型（普通 note、meeting 周会笔记等）都是
// 叶子；随手记收件箱也是目录。目录不能带正文。
const FolderType = "folder"

// FolderTitle 是目录节点标题为空时使用的默认标题。
const FolderTitle = "未命名目录"

// IsFolder 判断笔记是否是目录节点。
func (n *Note) IsFolder() bool {
	return n != nil && n.Type == FolderType
}

// validateNoteParent 校验 parent 能否作为笔记的父节点。
//
// parent 为空表示根层级，总是允许；非空时父节点必须存在（否则 ErrNotFound），
// 且必须是目录（否则 ErrInvalid）。
func (w *Workspace) validateNoteParent(parent string) error {
	if parent == "" {
		return nil
	}
	p, err := w.Get(parent)
	if err != nil {
		return err
	}
	if !p.IsFolder() {
		return fmt.Errorf("%w: 只有目录可以包含笔记，%q 是笔记（type=%s），不能作为父节点", ErrInvalid, parent, noteKindLabel(p))
	}
	return nil
}

// validateNoteBody 校验笔记正文：目录不能有正文，避免界面上出现可编辑正文的目录。
func validateNoteBody(typ, body string) error {
	if typ == FolderType && strings.TrimSpace(body) != "" {
		return fmt.Errorf("%w: 目录不能包含正文", ErrInvalid)
	}
	return nil
}

// noteKindLabel 返回错误信息里展示的笔记类型。
func noteKindLabel(n *Note) string {
	if n == nil || n.Type == "" {
		return defaultNoteType
	}
	return n.Type
}

// EnsureFolder 确保指定笔记是目录节点。
//
// 已经是目录时原样返回；否则就地改成 type=folder，ID、标题、顺序、正文与
// 子节点都不变（空标题补成默认目录名）。用于把老工作台里的容器笔记（例如
// 「周会」「随手记」收件箱）升级成目录，好让子笔记有合法的父节点。
func (w *Workspace) EnsureFolder(id string) (*Note, error) {
	note, err := w.Get(id)
	if err != nil {
		return nil, err
	}
	if note.IsFolder() {
		return note, nil
	}
	return w.Update(id, func(n *Note) {
		n.Type = FolderType
		n.Title = folderTitle(n.Title)
	})
}

// folderTitle 返回目录节点写回时的标题，空标题统一为默认目录名。
func folderTitle(title string) string {
	if strings.TrimSpace(title) == "" {
		return FolderTitle
	}
	return title
}

// folderTitleIfFolder 只在笔记是目录时把空标题补成默认目录名，其余类型原样返回。
func folderTitleIfFolder(typ, title string) string {
	if typ == FolderType {
		return folderTitle(title)
	}
	return title
}

// MigrateFolders 把「有子节点但不是目录」的笔记就地转成目录（type=folder）。
//
// 老工作台里父子关系只靠 parent 字段推导，任何笔记都可以有子节点。引入目录
// 模型后这些节点必须成为目录，否则它们的子节点在任何合法的树里都放不下。
// 迁移只改 type，ID、标题、顺序、子节点与正文全部保持不变（目录的正文在界面
// 上不展示，但直接删掉会丢用户数据，所以原样保留）。已经是目录的节点与叶子
// 笔记不动，因此可重复执行：没有需要迁移的节点时返回 0。
//
// 只处理 notes/ 下的笔记，todos/、people/、reports/ 不受影响。
// 返回本次迁移的节点数。
func (w *Workspace) MigrateFolders() (int, error) {
	metas, err := w.listMetas()
	if err != nil {
		return 0, err
	}
	// 有子节点的笔记 ID：按模型它们必须是目录。
	parents := make(map[string]bool, len(metas))
	for _, m := range metas {
		if m.Parent != "" {
			parents[m.Parent] = true
		}
	}

	migrated := 0
	now := time.Now().Truncate(time.Second)
	for _, m := range metas {
		if !parents[m.ID] || m.IsFolder() {
			continue
		}
		// 写回会丢掉正文，所以先读完整笔记。
		full, err := w.Get(m.ID)
		if err != nil {
			return migrated, err
		}
		full.Type = FolderType
		full.Title = folderTitle(full.Title)
		full.Updated = now
		if err := w.writeNote(full); err != nil {
			return migrated, err
		}
		migrated++
	}
	return migrated, nil
}

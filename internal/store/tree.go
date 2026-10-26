package store

import "sort"

// TreeNode 是笔记树中的一个节点。
type TreeNode struct {
	// Note 是节点对应的笔记元数据（不含正文）。
	Note *Note
	// Children 是子节点，按 order 再按 id 排序。
	Children []*TreeNode
	// Orphan 表示该节点的 parent 指向了不存在的笔记，被当作根节点处理。
	Orphan bool
	// Cycle 表示该节点因外部手改导致的父子环而被降为根节点。
	Cycle bool
}

// Tree 从全部笔记的 front matter 重建笔记树，返回根节点列表。
//
// parent 指向不存在笔记的孤儿节点会作为根节点返回，并置 Orphan 为 true；
// 外部手改造成的环不会导致死循环，环上的节点会被降为根节点并置 Cycle 为 true。
func (w *Workspace) Tree() ([]*TreeNode, error) {
	metas, err := w.listMetas()
	if err != nil {
		return nil, err
	}

	byID := make(map[string]*Note, len(metas))
	nodes := make(map[string]*TreeNode, len(metas))
	for _, m := range metas {
		byID[m.ID] = m
		nodes[m.ID] = &TreeNode{Note: m}
	}

	// 找出参与环的节点：从每个节点沿 parent 链向上走，若走回当前路径上
	// 已出现过的节点，则该位置起的路径节点都在环上。
	inCycle := map[string]bool{}
	for _, m := range metas {
		path := []string{}
		pos := map[string]int{}
		cur := m.ID
		for {
			if idx, ok := pos[cur]; ok {
				for _, id := range path[idx:] {
					inCycle[id] = true
				}
				break
			}
			pos[cur] = len(path)
			path = append(path, cur)
			n, ok := byID[cur]
			if !ok || n.Parent == "" {
				break
			}
			if _, ok := byID[n.Parent]; !ok {
				break
			}
			cur = n.Parent
		}
	}

	var roots []*TreeNode
	for _, m := range metas {
		node := nodes[m.ID]
		if inCycle[m.ID] {
			node.Cycle = true
			roots = append(roots, node)
			continue
		}
		if m.Parent == "" {
			roots = append(roots, node)
			continue
		}
		parent, ok := nodes[m.Parent]
		if !ok {
			node.Orphan = true
			roots = append(roots, node)
			continue
		}
		parent.Children = append(parent.Children, node)
	}

	sortTreeNodes(roots)
	for _, n := range nodes {
		sortTreeNodes(n.Children)
	}
	return roots, nil
}

// sortTreeNodes 按 order 再按 id 排序节点列表。
func sortTreeNodes(nodes []*TreeNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Note.Order != nodes[j].Note.Order {
			return nodes[i].Note.Order < nodes[j].Note.Order
		}
		return nodes[i].Note.ID < nodes[j].Note.ID
	})
}

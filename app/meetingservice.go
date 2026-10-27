package main

import (
	"time"

	"github.com/axu4/kuibu/internal/core"
	"github.com/axu4/kuibu/internal/store"
)

// OpenMeeting 打开本周周会笔记（没有就在「周会」节点下新建），返回笔记。
func (s *WorkspaceService) OpenMeeting() (NoteData, error) {
	ws := s.workspace()
	if ws == nil {
		return NoteData{}, errNotOpen()
	}
	note, _, err := core.OpenMeeting(ws, store.WeekOf(time.Now()))
	if err != nil {
		return NoteData{}, err
	}
	return toNoteData(note), nil
}

// TodoCandidates 预览笔记里可抽成待办的「- [ ] …」行；已抽过的带 todoId。
func (s *WorkspaceService) TodoCandidates(noteID string) ([]core.TodoCandidate, error) {
	ws := s.workspace()
	if ws == nil {
		return nil, errNotOpen()
	}
	note, err := ws.Get(noteID)
	if err != nil {
		return nil, err
	}
	return core.TodoCandidates(note, time.Now()), nil
}

// ExtractTodos 按勾选的键创建待办（source = 笔记 ID），返回新建条数。已抽过的行不重复。
func (s *WorkspaceService) ExtractTodos(noteID string, keys []string) (int, error) {
	ws := s.workspace()
	if ws == nil {
		return 0, errNotOpen()
	}
	if len(keys) == 0 {
		return 0, nil
	}
	created, err := core.ExtractTodos(ws, noteID, keys, time.Now())
	if len(created) > 0 {
		ids := make([]string, 0, len(created))
		for _, t := range created {
			ids = append(ids, t.ID)
		}
		s.emit(todoChangedEvent, TodoChangeEvent{IDs: ids})
	}
	return len(created), err
}

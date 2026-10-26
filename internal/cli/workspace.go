package cli

import (
	"github.com/axu4/kuibu/internal/config"
	"github.com/axu4/kuibu/internal/store"
)

// resolveWorkspace 按优先级定位工作台：
// --workspace > KUIBU_WORKSPACE > 用户配置默认工作台 > 从当前目录向上查找。
//
// 具体规则实现在 internal/config，与桌面 App 共用。
func resolveWorkspace(common *commonFlags, env func(string) string) (*store.Workspace, error) {
	return config.Resolve(env, common.workspace)
}

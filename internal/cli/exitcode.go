package cli

// 退出码约定：
//
//	0 成功；
//	7 NOT_FINAL（引用/导出了未定稿内容）；
//	8 NEEDS_CONFIRM（需人工确认）；
//	9 HUMAN_EDITS_EXIST（存在人工修改，拒绝覆盖）；
//	10 NOT_OPEN（未打开工作台）；
//	其他错误统一用 1。
const (
	OK              = 0
	Error           = 1
	NotFinal        = 7
	NeedsConfirm    = 8
	HumanEditsExist = 9
	NotOpen         = 10
)

// ExitCodeNames 是退出码到名称字符串的映射。
var ExitCodeNames = map[int]string{
	OK:              "OK",
	Error:           "ERROR",
	NotFinal:        "NOT_FINAL",
	NeedsConfirm:    "NEEDS_CONFIRM",
	HumanEditsExist: "HUMAN_EDITS_EXIST",
	NotOpen:         "NOT_OPEN",
}

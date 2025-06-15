package executor

import (
	"clampany/internal"
)

type HumanExecutor struct{}

func (h *HumanExecutor) Execute(t internal.Task, in string) (string, error) {
	// テスト用: 即return
	return "done", nil
}

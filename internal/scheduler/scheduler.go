package scheduler

import (
	"clampany/internal"
	"clampany/internal/executor"
	"clampany/internal/util"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

type Scheduler struct {
	ReadyCh     chan *internal.Task
	Wg          sync.WaitGroup
	MaxParallel int
}

func New(maxParallel int, numTasks int) *Scheduler {
	return &Scheduler{
		ReadyCh:     make(chan *internal.Task, numTasks),
		MaxParallel: maxParallel,
	}
}

func (s *Scheduler) Run(tasks []*internal.Task, execMap map[string]internal.Executor, runDir string, dEdges map[string][]string, roles []internal.Role) {
	taskMap := map[string]*internal.Task{}
	for _, t := range tasks {
		taskMap[t.Name] = t
	}
	// 依存カウント
	depCount := map[string]int{}
	for _, t := range tasks {
		depCount[t.Name] = len(t.DependsOn)
	}
	// 子タスク逆引き
	children := map[string][]string{}
	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			children[dep] = append(children[dep], t.Name)
		}
	}
	var mu sync.Mutex
	results := map[string]string{}
	failures := map[string]error{}
	numTasks := len(tasks)
	var doneCount int32
	// AIロールごとにペインを作成し、ロール名→ペインIDのマップを作る
	paneMap := map[string]string{}
	for _, r := range roles {
		if r.Type == internal.RoleAI {
			cmd := exec.Command("tmux", "split-window", "-v", "-P", "-F", "#{pane_id}", "bash")
			out, err := cmd.Output()
			if err == nil {
				paneID := strings.TrimSpace(string(out))
				paneMap[r.Name] = paneID
				exec.Command("tmux", "select-pane", "-t", paneID, "-T", r.Name).Run()
				// claudeを永続起動
				exec.Command("tmux", "send-keys", "-t", paneID, "claude", "Enter").Run()
			}
		}
	}
	exec.Command("tmux", "select-layout", "tiled").Run()

	// ロール名→RoleTypeのマップを作成
	roleTypeMap := map[string]internal.RoleType{}
	for _, r := range roles {
		roleTypeMap[r.Name] = r.Type
	}

	// AIExecutorはexecMapに格納しない
	for _, r := range roles {
		if r.Type != internal.RoleAI {
			execMap[r.Name] = &executor.HumanExecutor{}
		}
	}

	// ワーカープール起動
	for i := 0; i < s.MaxParallel; i++ {
		s.Wg.Add(1)
		go func(workerIdx int) {
			defer s.Wg.Done()
			for t := range s.ReadyCh {
				util.Info("[RUNNING] %s", t.Name)
				var out string
				var err error
				if roleTypeMap[t.Role] == internal.RoleAI {
					atomic.AddInt32(&doneCount, 1)
					if int(atomic.LoadInt32(&doneCount)) == numTasks {
						close(s.ReadyCh)
					}
					s.Wg.Done()
					continue // AIタスクは永続ワーカーで処理するためスキップ
				}
				// Human/Shellのみ従来通りexecMapを使う
				exec := execMap[t.Role]
				out, err = exec.Execute(*t, "")
				mu.Lock()
				if err != nil {
					failures[t.Name] = err
					util.Fail("%s: %v", t.Name, err)
				} else {
					results[t.Name] = out
					util.Success("%s 完了", t.Name)
					// 出力保存
					fpath := filepath.Join(runDir, "outputs", fmt.Sprintf("%s.md", t.Name))
					os.WriteFile(fpath, []byte(out), 0644)
					// clarification flow: needs_clarification検出
					if strings.Contains(out, "needs_clarification") {
						util.Info("clarificationフロー発動: %s", t.Name)
						// 簡易的にplanner1にclarificationタスクを追加
						clarTask := &internal.Task{
							Name:      t.Name + "_clarify",
							Role:      "planner1",
							Prompt:    "エンジニアからの質問: " + out,
							DependsOn: []string{t.Name},
						}
						mu.Lock()
						results[clarTask.Name] = ""
						mu.Unlock()
						s.ReadyCh <- clarTask
						// 元タスクを再実行（clarificationタスク完了後に）
						child := &internal.Task{
							Name:      t.Name + "_retry",
							Role:      t.Role,
							Prompt:    t.Prompt + "\n(clarification反映済み)",
							DependsOn: []string{clarTask.Name},
						}
						mu.Lock()
						results[child.Name] = ""
						mu.Unlock()
						s.ReadyCh <- child
					}
				}
				mu.Unlock()
				// 子タスクの依存カウントを減らし、0になればReadyChへ
				for _, child := range children[t.Name] {
					mu.Lock()
					depCount[child]--
					if depCount[child] == 0 {
						s.ReadyCh <- taskMap[child]
					}
					mu.Unlock()
				}
				atomic.AddInt32(&doneCount, 1)
				if int(atomic.LoadInt32(&doneCount)) == numTasks {
					close(s.ReadyCh)
				}
			}
		}(i)
	}
	// ワーカー起動後にReadyなタスクをチャネルに入れる
	for name, cnt := range depCount {
		if cnt == 0 {
			s.ReadyCh <- taskMap[name]
		}
	}
	s.Wg.Wait()
	// run.yaml保存
	summary := map[string]interface{}{
		"status":    "success",
		"timestamp": time.Now().Format(time.RFC3339),
	}
	if len(failures) > 0 {
		summary["status"] = "fail"
		summary["failures"] = failures
	}
	f, _ := os.Create(filepath.Join(runDir, "run.yaml"))
	yaml.NewEncoder(f).Encode(summary)
	f.Close()
}

func replaceInput(prompt, input string) string {
	return stringReplace(prompt, "{{input}}", input)
}

func stringReplace(s, old, new string) string {
	return fmt.Sprintf("%s", s)
	// TODO: strings.ReplaceAll(s, old, new) に置き換え
}

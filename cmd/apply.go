package cmd

import (
	"clampany/internal"
	"clampany/internal/dag"
	"clampany/internal/executor"
	"clampany/internal/loader"
	"clampany/internal/scheduler"
	"clampany/internal/util"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	tasksFile   string
	rolesFile   string
	maxParallel int
)

func init() {
	rootCmd.AddCommand(applyCmd)
	applyCmd.Flags().StringVarP(&tasksFile, "file", "f", "tasks.yaml", "Tasks YAML file")
	applyCmd.Flags().StringVarP(&rolesFile, "roles", "r", "roles.yaml", "Roles YAML file")
	applyCmd.Flags().IntVar(&maxParallel, "max-parallel", 2, "最大並列実行数")
}

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply tasks as defined in tasks.yaml",
	Run: func(cmd *cobra.Command, args []string) {
		util.Info("roles.yaml/taks.yamlを読み込み中...")
		roles, err := loader.LoadRoles(rolesFile)
		if err != nil {
			util.Fail("roles.yaml読み込み失敗: %v", err)
			os.Exit(1)
		}
		// tasks.yamlがなければ即終了
		if _, err := os.Stat(tasksFile); os.IsNotExist(err) {
			fmt.Println("[INFO] tasks.yamlが存在しないため、applyコマンドは何も実行しません")
			return
		}
		tasks, err := loader.LoadTasks(tasksFile)
		if err != nil {
			util.Fail("tasks.yaml読み込み失敗: %v", err)
			os.Exit(1)
		}
		d, err := dag.NewDAG(tasks)
		if err != nil {
			util.Fail("DAG構築失敗: %v", err)
			os.Exit(1)
		}
		_, err = dag.TopoSort(d)
		if err != nil {
			util.Fail("トポロジカルソート失敗: %v", err)
			os.Exit(1)
		}
		runID := util.NewUUID()
		runDir := filepath.Join("run-" + runID)
		os.MkdirAll(runDir, 0755)
		os.MkdirAll(filepath.Join(runDir, "outputs"), 0755)
		util.SetLogFile(filepath.Join(runDir, "log.txt"))
		defer util.CloseLogFile()
		util.Info("Runディレクトリ: %s", runDir)
		// Executorマッピング
		roleMap := map[string]internal.Role{}
		for _, r := range roles {
			roleMap[r.Name] = r
		}
		execMap := map[string]internal.Executor{}
		for _, r := range roles {
			if r.Type != internal.RoleAI {
				execMap[r.Name] = &executor.HumanExecutor{}
			}
		}
		util.Success("初期化完了。タスク数: %d", len(tasks))
		// スケジューラ起動
		scheduler := scheduler.New(maxParallel, len(tasks))
		taskPtrs := []*internal.Task{}
		for i := range tasks {
			taskPtrs = append(taskPtrs, &tasks[i])
		}
		scheduler.Run(taskPtrs, execMap, runDir, d.Edges, roles)
		util.Success("全タスク実行完了")
	},
}

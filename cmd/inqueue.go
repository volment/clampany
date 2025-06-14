package cmd

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

var inqueueMutex sync.Mutex
var inqueueCounter = map[string]int{}

var inqueueCmd = &cobra.Command{
	Use:   "inqueue <role> <message>",
	Short: "指定ロールの指示を<role>_queue.mdに追記",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		role := args[0]
		message := args[1]

		// roles.yamlがなくてもエラーにしない。instructions/や*_queue.mdからロール候補を自動検出
		var candidates []string
		// instructions/から
		entries, err := os.ReadDir("instructions")
		if err == nil {
			for _, entry := range entries {
				if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".md") && entry.Name() != "sufix.md" {
					roleName := strings.TrimSuffix(entry.Name(), ".md")
					if strings.HasPrefix(roleName, role) {
						candidates = append(candidates, roleName)
					}
				}
			}
		}
		// *_queue.mdからも
		queueEntries, err := os.ReadDir(".")
		for _, entry := range queueEntries {
			if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), "_queue.md") {
				roleName := strings.TrimSuffix(entry.Name(), "_queue.md")
				if strings.HasPrefix(roleName, role) {
					found := false
					for _, r := range candidates {
						if r == roleName {
							found = true
							break
						}
					}
					if !found {
						candidates = append(candidates, roleName)
					}
				}
			}
		}
		if len(candidates) == 0 {
			fmt.Printf("ロール %s が見つかりません\n", role)
			os.Exit(1)
		}
		// ラウンドロビンで割り当て
		inqueueMutex.Lock()
		idx := inqueueCounter[role] % len(candidates)
		inqueueCounter[role]++
		inqueueMutex.Unlock()
		assigned := candidates[idx]
		queueFile := fmt.Sprintf("clampany_queue/%s_queue.md", role)
		// 改行をスペースに置換して1行にまとめる
		message = strings.ReplaceAll(message, "\n", " ")
		message = strings.ReplaceAll(message, "\r", " ")
		section := fmt.Sprintf("\n[%s]\n%s\n", assigned, message)
		f, err := os.OpenFile(queueFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			fmt.Println(queueFile+"書き込み失敗:", err)
			os.Exit(1)
		}
		defer f.Close()
		_, err = f.WriteString(section)
		if err != nil {
			fmt.Println(queueFile+"追記失敗:", err)
			os.Exit(1)
		}
		fmt.Printf("[INQUEUE] %s → %s (%s)\n", assigned, message, queueFile)
	},
}

func init() {
	rootCmd.AddCommand(inqueueCmd)
}

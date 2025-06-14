package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var (
	sendRole   string
	sendPrompt string
)

func init() {
	rootCmd.AddCommand(sendCmd)
	sendCmd.Flags().StringVar(&sendRole, "role", "", "送信先ロール名")
	sendCmd.Flags().StringVar(&sendPrompt, "prompt", "", "送信するプロンプト")
}

var sendCmd = &cobra.Command{
	Use:   "send",
	Short: "指定ロールのtmuxペインにプロンプトを送信",
	Run: func(cmd *cobra.Command, args []string) {
		if sendRole == "" || sendPrompt == "" {
			fmt.Println("--roleと--promptは必須です")
			os.Exit(1)
		}
		// run/latest/panes.jsonからペインIDを取得
		f, err := os.Open("run/latest/panes.json")
		if err != nil {
			fmt.Println("panes.jsonが見つかりません")
			os.Exit(1)
		}
		defer f.Close()
		var paneMap map[string]string
		if err := json.NewDecoder(f).Decode(&paneMap); err != nil {
			fmt.Println("panes.jsonのデコードに失敗:", err)
			os.Exit(1)
		}
		paneID, ok := paneMap[sendRole]
		if !ok {
			fmt.Println("指定ロールのペインが見つかりません")
			os.Exit(1)
		}
		// send-keysでプロンプト＋Enter送信
		sendCmd := exec.Command("tmux", "send-keys", "-t", paneID, sendPrompt, "\r")
		err = sendCmd.Run()
		if err != nil {
			fmt.Println("tmux send-keys失敗:", err)
			os.Exit(1)
		}
		fmt.Printf("[SEND] %s → %s\n", sendRole, sendPrompt)
	},
}

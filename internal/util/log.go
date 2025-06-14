package util

import (
	"fmt"
	"os"
)

var logFile *os.File

func SetLogFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	logFile = f
	return nil
}

func CloseLogFile() {
	if logFile != nil {
		logFile.Close()
	}
}

func Info(msg string, args ...interface{}) {
	out := fmt.Sprintf("\033[34m[INFO]\033[0m "+msg+"\n", args...)
	fmt.Print(out)
	if logFile != nil {
		logFile.WriteString(stripColor(out))
	}
}

func Success(msg string, args ...interface{}) {
	out := fmt.Sprintf("\033[32m[DONE]\033[0m "+msg+"\n", args...)
	fmt.Print(out)
	if logFile != nil {
		logFile.WriteString(stripColor(out))
	}
}

func Fail(msg string, args ...interface{}) {
	out := fmt.Sprintf("\033[31m[FAIL]\033[0m "+msg+"\n", args...)
	fmt.Print(out)
	if logFile != nil {
		logFile.WriteString(stripColor(out))
	}
}

func stripColor(s string) string {
	// 簡易: エスケープシーケンス除去
	res := []rune{}
	skip := false
	for _, r := range s {
		if r == '\033' {
			skip = true
			continue
		}
		if skip && r == 'm' {
			skip = false
			continue
		}
		if !skip {
			res = append(res, r)
		}
	}
	return string(res)
}

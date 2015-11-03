package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	if len(os.Args) < 2 || os.Args[1][0] != '/' {
		printUsage()
		return
	}

	startPoint := os.Args[1]
	storage, err := extendScanWays(startPoint)
	if err != nil {
		panic(err)
	}
	plan := extendPlan(storage)
	extendPrint(plan)

	if len(os.Args) > 2 && os.Args[2] == "--do" {
		if extendDo(plan) {
			fmt.Println("NEED REBOOT AND START ME ONCE AGAIN.")
		} else {
			fmt.Println("OK")
		}
	}
}

func printUsage() {
	fmt.Printf(`Usage: %v <start_point> [--do]
start_point - path to block device or file system to extend
--do - do extending. Without it - print extend plan only.

The program print to stdout:
OK - if extended compele.
NEED REBOOT AND START ME ONCE AGAIN. - if need reboot and run command with same parameters
`, os.Args[0])
}

func cmd(cmd string, args ...string) (stdout, errout string, err error) {
	bufStd := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	command := exec.Command(cmd, args...)
	command.Stdout = bufStd
	command.Stderr = bufErr
	err = command.Run()
	return bufStd.String(), bufErr.String(), err
}

/*
execute command with args and return slice of strings.TrimSpace(line). Empty lines removed.
Возвращает stdout команды, разделенный на строки. У каждой строки пустые символы в начале/конце обрезаны, пустые строки
удалены. stderr и код ответа не учитываются
*/
func cmdTrimLines(command string, args ...string) []string {
	res, _, _ := cmd(command, args...)
	lines := make([]string, 0)
	for _, line := range strings.Split(res, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		} else {
			lines = append(lines, line)
		}
	}
	return lines
}

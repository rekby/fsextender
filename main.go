package fsextender

import (
	"bytes"
	"fmt"
	"github.com/rekby/fsextender/Godeps/_workspace/src/github.com/ogier/pflag"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const DEBUG = false

//go:generate go-bindata README.md usage.txt
func Main() int {
	showHelp := pflag.BoolP("help", "h", false, "Show long usage manual")
	showReadme := pflag.Bool("readme", false, "Show readme")
	do := pflag.Bool("do", false, "Execute plan instead of print it")
	filter := pflag.StringP("filter", "f", FILTER_LVM_ALREADY_PLACED, "filter of disks, which use for partition extends")
	pflag.Parse()

	if *showHelp {
		txt, err := usageTxtBytes()
		if err != nil {
			log.Println(err)
			return 11
		}
		fmt.Println(string(txt))
		return 0
	}

	if *showReadme {
		txt, err := readmeMdBytes()
		if err != nil {
			log.Println(err)
			return 11
		}
		fmt.Println(string(txt))
		return 0
	}

	if pflag.NArg() != 1 || !filepath.IsAbs(pflag.Arg(0)) {
		printShortUsage()
		return 11
	}

	startPoint := pflag.Arg(0)
	storage, err := extendScanWays(startPoint)
	//	fmt.Println("SCAN PLAN:")
	//	extendPrint(storage)
	//	fmt.Println()
	//	fmt.Println()
	//	fmt.Println()
	if err != nil {
		panic(err)
	}
	plan, err := extendPlan(storage, *filter)
	if err != nil {
		log.Println("Error while make extend plan:", err)
		return 11
	}

	if *do {
		if extendDo(plan) {
			fmt.Println("NEED REBOOT AND START ME ONCE AGAIN.")
			return 128
		} else {
			fmt.Println("OK")
			return 0
		}
	} else {
		extendPrint(plan)
		return 0
	}
}

func printShortUsage() {
	fmt.Printf(`Short usage: %v [options] <start_point>
Detect result:
OK - if extended compele. Return code 0.
NEED REBOOT AND START ME ONCE AGAIN. - if need reboot and run command with same parameters. Return code 128.

0 < Code < 128 mean error exit. (Now it print usages and panic only).

Options:
`, os.Args[0])
	pflag.PrintDefaults()
}

func cmd(cmd string, args ...string) (stdout, errout string, err error) {
	bufStd := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	command := exec.Command(cmd, args...)
	command.Stdout = bufStd
	command.Stderr = bufErr
	err = command.Run()
	if DEBUG {
		log.Printf("CMD: '%v' '%v'\n", cmd, strings.Join(args, "' '"))
		log.Printf("RES:\n%v\nERR:\n%v\nERROR:\n%v\n\n", bufStd.String(), bufErr.String(), err)
	}
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

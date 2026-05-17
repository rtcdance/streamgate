package main

import (
	"fmt"
	"strings"
)

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	faint  = "\033[2m"
	cyan   = "\033[36m"
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	blue   = "\033[34m"
)

func header(title string) {
	fmt.Println()
	fmt.Printf("  %s%s%s\n", bold, title, reset)
	fmt.Println("  " + faint + "──────────────────────────────────────────" + reset)
	fmt.Println()
}

func step(n int, desc string) {
	fmt.Printf("  %s▸%s %s%s\n", cyan, reset, bold, desc)
	fmt.Println()
}

func code(s string) {
	for _, line := range strings.Split(s, "\n") {
		fmt.Printf("    %s%s%s\n", faint, line, reset)
	}
	fmt.Println()
}

func ok(msg string) {
	fmt.Printf("  %s✓%s %s\n\n", green, reset, msg)
}

func fail(msg string) {
	fmt.Printf("  %s✗%s %s\n\n", red, reset, msg)
}

func info(msg string) {
	fmt.Printf("  %s%s%s\n", yellow, msg, reset)
}

func detail(label, value string) {
	fmt.Printf("  %s%-12s%s %s\n", faint, label+":", reset, value)
}

func separator() {
	fmt.Println("  " + faint + "──────────────────────────────────────────" + reset)
	fmt.Println()
}

func promptExit() {
	fmt.Print(faint + "  按 Enter 返回菜单..." + reset)
	if _, err := fmt.Scanln(); err != nil {
		return
	}
}

func section(title string) {
	fmt.Printf("\n  %s── %s ──%s\n\n", faint, title, reset)
}

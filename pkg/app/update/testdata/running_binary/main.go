package main

import (
	"bufio"
	"fmt"
	"os"
)

var version = "unset"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("rzm version " + version)
		return
	}
	if len(os.Args) != 2 || os.Args[1] != "hold" {
		os.Exit(2)
	}
	fmt.Println("ready " + version)
	input := bufio.NewScanner(os.Stdin)
	for input.Scan() {
		if input.Text() != "ping" {
			os.Exit(2)
		}
		fmt.Println("alive " + version)
	}
	if input.Err() != nil {
		os.Exit(1)
	}
}

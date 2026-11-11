//go:build !windows

// Установщик есть только для Windows; на Linux — install.sh из архива (sudo ./install.sh).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "Для Linux: распакуйте архив и запустите sudo ./install.sh")
	os.Exit(1)
}

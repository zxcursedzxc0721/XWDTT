package main

import (
	"fmt"
	"os"

	"pwdtt/backend"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-v":
			fmt.Println(backend.Version)
			return
		case "help", "--help", "-h":
			usage()
			return
		}
	}
	usage()
}

func usage() {
	fmt.Printf("PWDTT %s — консольный VPN-клиент\n\n", backend.Version)
	fmt.Println("Этап 1 (отвязка от GUI) завершён. Команды CLI появятся на этапе 2.")
	fmt.Println("План работ: PLAN.md в корне репозитория.")
}

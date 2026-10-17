// Job de exemplo: processa, grava o resultado no volume compartilhado e termina.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	fmt.Println("payload recebido:", os.Getenv("MDD_PAYLOAD"))

	fmt.Println("processando...")
	time.Sleep(time.Second)

	path := filepath.Join(os.Getenv("MDD_SHARED"), "resultado.txt")
	if err := os.WriteFile(path, []byte("ok\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	fmt.Println("resultado gravado em", path)

	// Dispara as setas "resultado_pronto" já agora, antes de terminar.
	fmt.Println(`::mdd emit resultado_pronto {"arquivo": "resultado.txt"}`)

	time.Sleep(time.Second)
	fmt.Println("fim")
}

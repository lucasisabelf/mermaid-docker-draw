// Serviço HTTP de exemplo: responde "olá" na porta $PORT.
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		fmt.Fprintf(w, "olá do Go! (retângulo %s)\n", os.Getenv("MDD_NODE_ID"))
	})

	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("ouvindo na porta", port)
	// Avisa o fluxo: setas "pronto" disparam o próximo retângulo.
	fmt.Println("::mdd emit pronto")
	log.Fatal(http.Serve(ln, nil))
}

package main

import (
	"fmt"
	"net/http"
)

func main() {
	const local_port string = "6969"
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, err := fmt.Fprintln(w, "10 euro op gele kip!")
		if err != nil {
			return
		}
	})
	fmt.Printf("Server started on http://localhost:%s\n", local_port)
	err := http.ListenAndServe(":"+local_port, nil)
	if err != nil {
		return
	}
}

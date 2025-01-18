package main

import (
	"fmt"
	"html/template"
	"net/http"
)

type RaceInfo struct {
	Id           int
	Name         string
	ChickenNames []string
}

type User struct {
	Name string
	Age  int
}
type PageData struct {
	Title    string
	UserData User
	Races    RaceInfo
}

func main() {
	const local_port string = "6969"

	tmpl := template.Must(template.ParseGlob("src/web/templates/*.gohtml"))

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("src/web/static"))))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data := PageData{
			Title: "Scramble Run",
			UserData: User{
				Name: "test_user",
				Age:  99,
			},
			Races: RaceInfo{
				Id:           1,
				Name:         "test_race",
				ChickenNames: []string{"test_chicken1", "test_chicken2"},
			},
		}

		err := tmpl.ExecuteTemplate(w, "home.gohtml", data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	fmt.Printf("Server started on http://localhost:%s\n", local_port)
	err := http.ListenAndServe(":"+local_port, nil)
	if err != nil {
		return
	}
}

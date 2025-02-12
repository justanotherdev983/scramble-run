package main

import (
	"database/sql"
	_ "encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type RaceInfo struct {
	Id           int
	Name         string
	Winner       string
	ChickenNames []string
	Date         time.Time
}

type User struct {
	Name string
	Age  int
}
type PageData struct {
	Title    string
	UserData User
	Races    []RaceInfo
}

func init_database() *sql.DB {
	db, err := sql.Open("sqlite3", "src/internal/database/scramble.db")
	if err != nil {
		log.Fatalf("Failed to connect to the database: %v", err)
		return nil
	}

	// Don't re-initialize or insert data if it's already populated
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM races").Scan(&count)
	if err != nil {
		log.Fatalf("Failed to check races table: %v", err)
		return nil
	}

	if count == 0 {
		// Insert data only if the table is empty
		sql_file, err := ioutil.ReadFile("src/internal/database/init_database.sql")
		if err != nil {
			log.Fatalf("Failed to read SQL initialization file: %v", err)
			return nil
		}

		_, err = db.Exec(string(sql_file))
		if err != nil {
			log.Fatalf("Failed to initialize the database: %v", err)
			return nil
		}

		fmt.Println("Database initialized successfully")
	}

	return db
}

func get_races(db *sql.DB) []RaceInfo {
	rows, err := db.Query("SELECT id, name, date, winner FROM races;")
	if err != nil {
		log.Fatalf("Failed to get races with error: %v", err)
	}
	defer rows.Close()

	var races []RaceInfo

	for rows.Next() {
		var race RaceInfo

		err = rows.Scan(&race.Id, &race.Name, &race.Date, &race.Winner)
		if err != nil {
			log.Fatalf("Failed to scan row with error: %v", err)
		}

		log.Printf("Scanned race: ID=%d, Name=%s, Winner=%s, Date=%v",
			race.Id, race.Name, race.Winner, race.Date)

		races = append(races, race)
	}
	return races
}

func main() {
	const local_port string = "6969"

	db := init_database()
	defer db.Close()

	tmpl := template.Must(template.ParseGlob("src/web/templates/*.gohtml"))

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("src/web/static"))))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data := PageData{
			Title: "Scramble Run",
			UserData: User{
				Name: "test_user",
				Age:  99,
			},
			Races: get_races(db),
		}

		err := tmpl.ExecuteTemplate(w, "home.gohtml", data)
		if err != nil {
			return
		}
	})

	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		data := PageData{
			Title: "Scramble Run",
			UserData: User{
				Name: "test_user",
				Age:  99,
			},
			Races: get_races(db),
		}

		err := tmpl.ExecuteTemplate(w, "login.gohtml", data)
		if err != nil {
			return
		}
	})

	http.HandleFunc("/add-race", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			raceName := r.FormValue("name")
			winner := r.FormValue("winner")
			chickenNames := r.FormValue("chicken_names")

			var chickenNamesJSON string
			if chickenNames != "" {
				chickenNamesJSON = fmt.Sprintf("[\"%s\"]", chickenNames)
			}

			_, err := db.Exec("INSERT INTO races (name, winner, chicken_names) VALUES (?, ?, ?)",
				raceName, winner, chickenNamesJSON)
			if err != nil {
				log.Printf("Error inserting new race: %v", err)
				http.Error(w, "Failed to add race", http.StatusInternalServerError)
				return
			}

			data := PageData{
				Title: "Scramble Run",
				Races: get_races(db),
			}
			err = tmpl.ExecuteTemplate(w, "add-race.gohtml", data)
			if err != nil {
				log.Printf("Error rendering add-race template: %v", err)
				http.Error(w, "Failed to render page", http.StatusInternalServerError)
				return
			}
		} else {
			// Handle GET request and render the form
			data := PageData{
				Title: "Add a New Race",
				Races: get_races(db),
			}
			err := tmpl.ExecuteTemplate(w, "add-race.gohtml", data)
			if err != nil {
				log.Printf("Error rendering add-race template: %v", err)
				http.Error(w, "Failed to render page", http.StatusInternalServerError)
				return
			}
		}
	})

	fmt.Printf("Server started on http://localhost:%s\n", local_port)
	err := http.ListenAndServe(":"+local_port, nil)
	if err != nil {
		return
	}
}

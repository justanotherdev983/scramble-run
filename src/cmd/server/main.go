package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
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

type Chicken struct {
	ID       int
	Name     string
	Color    string
	Odds     float64
	Lane     int
	Progress float64
}

type ActiveRace struct {
	Chickens []Chicken
}

type User struct {
	Name string
	Age  int
}

type PageData struct {
	Title             string
	UserData          User
	Races             []RaceInfo
	Chickens          []Chicken  // For betting panel
	ActiveRace        ActiveRace // Current race in progress
	NextRaceTime      string     // Time until next race
	PotentialWinnings float64    // Calculated winnings
}

type WinningsCalc struct {
	Amount    float64
	ChickenID int
}

type BetResponse struct {
	Success     bool
	Message     string
	NewBalance  float64
	BetAmount   float64
	ChickenName string
}

func init_database() *sql.DB {
	db, err := sql.Open("sqlite3", "src/internal/database/scramble.db")
	if err != nil {
		log.Printf("Failed to connect to the database: %v", err)
		return nil
	}

	// Don't re-initialize or insert data if it's already populated
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM races").Scan(&count)
	if err != nil {
		log.Printf("Failed to check races table: %v", err)
		return nil
	}

	if count == 0 {
		// Insert data only if the table is empty
		sql_file, err := ioutil.ReadFile("src/internal/database/init_database.sql")
		if err != nil {
			log.Printf("Failed to read SQL initialization file: %v", err)
			return nil
		}

		_, err = db.Exec(string(sql_file))
		if err != nil {
			log.Printf("Failed to initialize the database: %v", err)
			return nil
		}

		fmt.Println("Database initialized successfully")
	}

	return db
}

func get_races(db *sql.DB) []RaceInfo {
	rows, err := db.Query("SELECT id, name, date, winner FROM races;")
	if err != nil {
		log.Printf("Failed to get races with error: %v", err)
		return nil
	}
	defer rows.Close()

	var races []RaceInfo

	for rows.Next() {
		var race RaceInfo

		err = rows.Scan(&race.Id, &race.Name, &race.Date, &race.Winner)
		if err != nil {
			log.Printf("Failed to scan row with error: %v", err)
			continue // Skip to the next row
		}

		log.Printf("Scanned race: ID=%d, Name=%s, Winner=%s, Date=%v",
			race.Id, race.Name, race.Winner, race.Date)

		races = append(races, race)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Error iterating through rows: %v", err)
		return nil
	}

	return races
}

func main() {
	const local_port string = "6969"

	db := init_database()
	if db == nil {
		log.Fatal("Database initialization failed") // Exit if database fails to initialize
		return
	}
	defer db.Close()

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("src/web/static"))))

	tmpl := template.Must(template.ParseFiles(
		"src/web/templates/base.gohtml",
		"src/web/templates/home.gohtml",
	))

	// Update your handler to execute the base template
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		chickens := []Chicken{
			{ID: 1, Name: "Henrietta", Color: "red", Odds: 2.5, Lane: 10, Progress: 0},
			{ID: 2, Name: "Cluck Norris", Color: "blue", Odds: 3.0, Lane: 50, Progress: 0},
			{ID: 3, Name: "Foghorn", Color: "green", Odds: 4.0, Lane: 90, Progress: 0},
		}
		// Your existing data setup...
		data := PageData{
			Title: "Scramble Run",
			UserData: User{
				Name: "test_user",
				Age:  99,
			},
			Races:    get_races(db),
			Chickens: chickens,
			ActiveRace: ActiveRace{
				Chickens: chickens,
			},
			NextRaceTime:      "5 minutes",
			PotentialWinnings: 100.0,
		}

		// Execute the base template instead of home directly
		err := tmpl.ExecuteTemplate(w, "base.gohtml", data)
		if err != nil {
			log.Printf("Template execution error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
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
		err := tmpl.ExecuteTemplate(w, "base.gohtml", data)
		if err != nil {
			log.Printf("Template execution error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
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

			// Use parameterized query to prevent SQL injection
			stmt, err := db.Prepare("INSERT INTO races (name, winner, chicken_names) VALUES (?, ?, ?)")
			if err != nil {
				log.Printf("Error preparing SQL statement: %v", err)
				http.Error(w, "Failed to add race", http.StatusInternalServerError)
				return
			}
			defer stmt.Close()

			_, err = stmt.Exec(raceName, winner, chickenNamesJSON)
			if err != nil {
				log.Printf("Error inserting new race: %v", err)
				http.Error(w, "Failed to add race", http.StatusInternalServerError)
				return
			}

			http.Redirect(w, r, "/", http.StatusSeeOther) // Redirect to home page
			return

		} else {
			// Handle GET request and render the form
			data := PageData{
				Title: "Add a New Race",
				Races: get_races(db),
			}

			err := tmpl.ExecuteTemplate(w, "add_race.gohtml", data)
			if err != nil {
				log.Printf("Error rendering add-race template: %v", err)
				http.Error(w, "Failed to render page", http.StatusInternalServerError)
				return
			}
		}
	})
	http.HandleFunc("/select-chicken/", func(w http.ResponseWriter, r *http.Request) {
		// Extract the chicken ID from the URL
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 3 {
			http.Error(w, "Invalid chicken ID", http.StatusBadRequest)
			return
		}

		chickenID, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil {
			http.Error(w, "Invalid chicken ID", http.StatusBadRequest)
			return
		}

		// Get bet amount from query params or use default
		betAmount := 10.0 // default value
		if amount := r.URL.Query().Get("betAmount"); amount != "" {
			if parsed, err := strconv.ParseFloat(amount, 64); err == nil {
				betAmount = parsed
			}
		}

		// Calculate potential winnings (you'll want to get actual odds from your chicken data)
		// This is just an example calculation
		winnings := WinningsCalc{
			Amount:    betAmount * 2.5, // Replace with actual odds
			ChickenID: chickenID,
		}

		// Return just the winnings calculation div
		w.Header().Set("Content-Type", "text/html")
		template.Must(template.New("winnings").Parse(`
        <div class="winnings-display" id="winnings-calc">
            <p>Potential Win:</p>
            <span class="winnings-amount">{{.Amount}} Credits</span>
            <input type="hidden" name="selectedChicken" value="{{.ChickenID}}" />
        </div>
    `)).Execute(w, winnings)
	})

	http.HandleFunc("/place-bet", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse form data
		err := r.ParseForm()
		if err != nil {
			http.Error(w, "Failed to parse form data", http.StatusBadRequest)
			return
		}

		// Get bet amount and selected chicken
		betAmount, err := strconv.ParseFloat(r.FormValue("betAmount"), 64)
		if err != nil {
			http.Error(w, "Invalid bet amount", http.StatusBadRequest)
			return
		}

		_, err = strconv.Atoi(r.FormValue("selectedChicken"))
		if err != nil {
			http.Error(w, "Invalid chicken selection", http.StatusBadRequest)
			return
		}

		// Here you would typically:
		// 1. Validate the user has enough credits
		// 2. Process the bet in your database
		// 3. Update the user's balance
		// For now, we'll just return a success message

		response := BetResponse{
			Success:     true,
			Message:     "Bet placed successfully!",
			NewBalance:  1000.00 - betAmount, // Replace with actual balance calculation
			BetAmount:   betAmount,
			ChickenName: "Selected Chicken", // Replace with actual chicken name
		}

		// Return a success message
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("HX-Trigger", "betPlaced") // Optional: trigger an event for other updates

		tmpl := template.Must(template.New("betResponse").Parse(`
        <div class="bet-response" id="bet-response">
            {{if .Success}}
                <div class="alert alert-success">
                    <p>{{.Message}}</p>
                    <p>Bet placed: {{.BetAmount}} credits on {{.ChickenName}}</p>
                    <p>New balance: {{.NewBalance}} credits</p>
                </div>
            {{else}}
                <div class="alert alert-error">
                    <p>{{.Message}}</p>
                </div>
            {{end}}
        </div>
    `))

		err = tmpl.Execute(w, response)
		if err != nil {
			http.Error(w, "Failed to render response", http.StatusInternalServerError)
			return
		}
	})

	fmt.Printf("Server started on http://localhost:%s\n", local_port)
	err := http.ListenAndServe(":"+local_port, nil)
	if err != nil {
		return
	}
}

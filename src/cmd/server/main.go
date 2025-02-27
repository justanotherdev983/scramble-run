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

const local_port string = "6969"

var (
	db            *sql.DB
	baseTemplate  *template.Template
	homeTemplate  *template.Template
	loginTemplate *template.Template
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

func init() {
	var err error

	// Initialize database connection
	db = init_database()
	if db == nil {
		log.Fatal("Database initialization failed")
		return
	}

	// Parse base template
	baseTemplate, err = template.ParseFiles("src/web/templates/base.gohtml")
	if err != nil {
		log.Fatalf("Error parsing base template: %v", err)
		return
	}

	// Clone base template and parse home template
	homeTemplate, err = template.Must(baseTemplate.Clone()).ParseFiles(
		"src/web/templates/home.gohtml",
	)
	if err != nil {
		log.Fatalf("Error parsing home template: %v", err)
		return
	}

	// Clone base template and parse login template
	loginTemplate, err = template.Must(baseTemplate.Clone()).ParseFiles(
		"src/web/templates/login.gohtml",
	)
	if err != nil {
		log.Fatalf("Error parsing login template: %v", err)
		return
	}

	log.Println("Templates loaded successfully")
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

func homeHandler(w http.ResponseWriter, r *http.Request) {
	chickens := []Chicken{
		{ID: 1, Name: "Henrietta", Color: "red", Odds: 2.5, Lane: 10, Progress: 0},
		{ID: 2, Name: "Cluck Norris", Color: "blue", Odds: 3.0, Lane: 50, Progress: 0},
		{ID: 3, Name: "Foghorn", Color: "green", Odds: 4.0, Lane: 90, Progress: 0},
	}
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

	err := homeTemplate.ExecuteTemplate(w, "base.gohtml", data)
	if err != nil {
		log.Printf("Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title: "Scramble Run",
		UserData: User{
			Name: "test_user",
			Age:  99,
		},
		Races: get_races(db),
	}
	err := loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
	if err != nil {
		log.Printf("Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func selectChickenHandler(w http.ResponseWriter, r *http.Request) {
	// Extract the chicken ID from the URL path
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 3 {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}

	chickenIDStr := pathParts[2]
	chickenID, err := strconv.Atoi(chickenIDStr)
	if err != nil {
		http.Error(w, "Invalid chicken ID", http.StatusBadRequest)
		return
	}

	// Find the selected chicken information
	var selectedChicken Chicken
	found := false

	for _, chicken := range []Chicken{
		{ID: 1, Name: "Henrietta", Color: "red", Odds: 2.5, Lane: 10, Progress: 0},
		{ID: 2, Name: "Cluck Norris", Color: "blue", Odds: 3.0, Lane: 50, Progress: 0},
		{ID: 3, Name: "Foghorn", Color: "green", Odds: 4.0, Lane: 90, Progress: 0},
	} {
		if chicken.ID == chickenID {
			selectedChicken = chicken
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "Chicken not found", http.StatusBadRequest)
		return
	}

	// Since we're targeting #winnings-calc, we should return the winnings calculation
	// Get the bet amount from the query parameters (if provided)
	betAmount := 10.0 // Default value
	betAmountStr := r.URL.Query().Get("betAmount")
	if betAmountStr != "" {
		parsedAmount, err := strconv.ParseFloat(betAmountStr, 64)
		if err == nil && parsedAmount > 0 {
			betAmount = parsedAmount
		}
	}

	// Calculate potential winnings
	potentialWinnings := betAmount * selectedChicken.Odds

	// Return the winnings calculation in HTML format
	w.Header().Set("Content-Type", "text/html")

	tmpl := template.Must(template.New("winningsCalc").Parse(`
			<div class="winnings-display" id="winnings-calc">
				<p>Potential Win:</p>
				<span class="winnings-amount">{{.Amount}} Credits</span>
				<input type="hidden" name="selectedChicken" value="{{.ChickenID}}" />
			</div>
			`))

	winnings := WinningsCalc{
		Amount:    potentialWinnings,
		ChickenID: chickenID,
	}

	err = tmpl.Execute(w, winnings)
	if err != nil {
		log.Printf("Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func calculateWinningsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}

	betAmountStr := r.Form.Get("betAmount")
	if betAmountStr == "" {
		http.Error(w, "Bet amount is required", http.StatusBadRequest)
		return
	}

	betAmount, err := strconv.ParseFloat(betAmountStr, 64)
	if err != nil {
		http.Error(w, "Invalid bet amount", http.StatusBadRequest)
		return
	}

	chickenIDStr := r.Form.Get("selectedChicken")
	if chickenIDStr == "" {
		http.Error(w, "Chicken ID is required", http.StatusBadRequest)
		return
	}

	chickenID, err := strconv.Atoi(chickenIDStr)
	if err != nil {
		http.Error(w, "Invalid chicken ID", http.StatusBadRequest)
		return
	}

	// Retrieve chicken odds from database or in-memory data
	var chickenOdds float64
	for _, chicken := range []Chicken{
		{ID: 1, Name: "Henrietta", Color: "red", Odds: 2.5, Lane: 10, Progress: 0},
		{ID: 2, Name: "Cluck Norris", Color: "blue", Odds: 3.0, Lane: 50, Progress: 0},
		{ID: 3, Name: "Foghorn", Color: "green", Odds: 4.0, Lane: 90, Progress: 0},
	} {
		if chicken.ID == chickenID {
			chickenOdds = chicken.Odds
			break
		}
	}

	if chickenOdds == 0 {
		http.Error(w, "Chicken not found", http.StatusBadRequest)
		return
	}

	winnings := WinningsCalc{
		Amount:    betAmount * chickenOdds,
		ChickenID: chickenID,
	}

	w.Header().Set("Content-Type", "text/html")

	winningsTemplate := template.Must(template.New("winnings").Parse(`
        <div class="winnings-display" id="winnings-calc">
            <p>Potential Win:</p>
            <span class="winnings-amount">{{.Amount}} Credits</span>
            <input type="hidden" name="selectedChicken" value="{{.ChickenID}}" />
        </div>
    `))

	err = winningsTemplate.Execute(w, winnings)
	if err != nil {
		log.Printf("Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func placeBetHandler(w http.ResponseWriter, r *http.Request) {
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

	chickenIDStr := r.FormValue("selectedChicken")
	if chickenIDStr == "" {
		http.Error(w, "Invalid chicken selection", http.StatusBadRequest)
		return
	}

	// We need some form validation:
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
}

func main() {
	defer db.Close()

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("src/web/static"))))

	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/select-chicken/", selectChickenHandler)
	http.HandleFunc("/calculate-winnings", calculateWinningsHandler)
	http.HandleFunc("/place-bet", placeBetHandler)

	fmt.Printf("Server started on http://localhost:%s\n", local_port)
	err := http.ListenAndServe(":"+local_port, nil)
	if err != nil {
		return
	}
}

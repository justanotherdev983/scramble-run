package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// Constants
const (
	local_port          string = "6969"
	raceInterval               = 1 * time.Minute
	raceDuration               = 20 * time.Second
	RaceStatusScheduled string = "Scheduled"
	RaceStatusRunning   string = "Running"
	RaceStatusFinished  string = "Finished"
	RaceStatusNoRace    string = "NoRace"
)

// Global Variables
var (
	db                  *sql.DB
	baseTemplate        *template.Template
	homeTemplate        *template.Template
	raceTemplate        *template.Template
	loginTemplate       *template.Template
	signupTemplate      *template.Template
	contactTemplate     *template.Template
	aboutUsTemplate     *template.Template
	betResponseTemplate *template.Template
	raceInfoTemplate    *template.Template

	availableChickens = []Chicken{
		{ID: 1, Name: "Henrietta", Color: "red", Odds: 2.5, Lane: 10, Progress: 0},
		{ID: 2, Name: "Cluck Norris", Color: "blue", Odds: 3.0, Lane: 50, Progress: 0},
		{ID: 3, Name: "Foghorn", Color: "green", Odds: 4.0, Lane: 90, Progress: 0},
	}

	raceMutex          sync.Mutex
	currentRaceDetails *RaceInfo
	nextRaceStartTime  time.Time
	raceTicker         *time.Ticker
	raceEndTimer       *time.Timer
	isRaceSystemActive bool = false
)

// init initializes database connection, templates, and seeds random number generator.
func init() {
	var err error
	db = init_database() // From db_utils.go
	if db == nil {
		log.Fatal("Database initialization failed")
		return
	}
	rand.Seed(time.Now().UnixNano())

	// Initialize SMTP/Contact settings
	smtpHost = getEnvOrDefault("SMTP_HOST", "smtp.gmail.com")
	smtpPort = getEnvOrDefault("SMTP_PORT", "587")
	smtpUsername = getEnvOrDefault("SMTP_USERNAME", "")                          // Fill with your actual Gmail address or App Password
	smtpPassword = getEnvOrDefault("SMTP_PASSWORD", "")                          // Fill with your actual Gmail App Password
	toEmail = getEnvOrDefault("CONTACT_EMAIL", "your-company-email@example.com") // Email to receive contact messages

	baseTemplate, err = template.ParseFiles("src/web/templates/base.gohtml")
	if err != nil {
		log.Fatalf("Error parsing base template: %v", err)
	}

	homeTemplate, err = template.Must(baseTemplate.Clone()).ParseFiles("src/web/templates/home.gohtml")
	if err != nil {
		log.Fatalf("Error parsing home template: %v", err)
	}

	raceTemplate, err = template.Must(baseTemplate.Clone()).ParseFiles("src/web/templates/races.gohtml")
	if err != nil {
		log.Fatalf("Error parsing base template: %v", err)
	}

	loginTemplate, err = template.Must(baseTemplate.Clone()).ParseFiles("src/web/templates/login.gohtml")
	if err != nil {
		log.Fatalf("Error parsing login template: %v", err)
	}

	signupTemplate, err = template.Must(baseTemplate.Clone()).ParseFiles("src/web/templates/signup.gohtml")
	if err != nil {
		log.Fatalf("Error parsing signup template: %v", err)
	}

	contactTemplate, err = template.Must(baseTemplate.Clone()).ParseFiles("src/web/templates/contact.gohtml")
	if err != nil {
		log.Fatalf("Error parsing signup template: %v", err)
	}

	aboutUsTemplate, err = template.Must(baseTemplate.Clone()).ParseFiles("src/web/templates/about-us.gohtml")
	if err != nil {
		log.Fatalf("Error parsing signup template: %v", err)
	}

	betResponseTemplate = template.Must(template.New("betResponse").Parse(`
		{{/* This is the content for #bet-response-area */}}
		<div class="bet-response" id="bet-response-content">
			{{if .Success}}
				<div class="alert alert-success">
					<p>{{.Message}}</p>
					<p>Bet placed: {{printf "%.2f" .BetAmount}} credits on {{.ChickenName}}</p>
					<p>New balance: {{printf "%.2f" .NewBalance}} credits</p>
				</div>
			{{else}}
				<div class="alert alert-danger">
					<p>{{.Message}}</p>
					{{if ge .NewBalance 0.0}}
					<p>Your balance: {{printf "%.2f" .NewBalance}} credits</p>
					{{end}}
				</div>
			{{end}}
		</div>
		<span id="user-balance-display" hx-swap-oob="true">{{printf "%.2f" .NewBalance}}</span>
	`))

	raceInfoTemplate = template.Must(template.New("raceInfoSnippet").Parse(`
		{{/* This is the entire new innerHTML for div#race-timer-dynamic-area */}}
		<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="race-timer-icon">
			<circle cx="12" cy="12" r="10"></circle>
			<polyline points="12 6 12 12 16 14"></polyline>
		</svg>
		<span class="race-timer-prefix">
			{{ if .IsRaceRunning }}
				Race in Progress:
			{{ else if .CountdownStr }}
				Next race in:
			{{ else if .StatusMsg }}
			{{ end }}
		</span>
		<span class="race-timer-countdown">
			{{if .CountdownStr}}
				{{.CountdownStr}}
			{{else if .StatusMsg}}
				{{.StatusMsg}}
			{{else}}
				--:--
			{{end}}
		</span>
		{{if .RaceName}}
			<span class="race-timer-racename">({{ .RaceName }})</span>
		{{end}}
		<br> 
		<span class="race-timer-bettingstatus">
			{{if .IsBettingOpen}}
				Betting is Open!
			{{else if .IsRaceRunning}}
				Betting Closed (Race Running)
			{{else}}
				Betting is Closed
			{{end}}
		</span>
		{{if .UserLoggedIn }}
		<span id="user-balance-display" hx-swap-oob="innerHTML">
			{{printf "%.2f" .CurrentUserBalance}}
		</span>
		{{end}}
	`))
	log.Println("Templates loaded successfully")
}

// main is the entry point of the application.
func main() {
	if db == nil {
		log.Fatal("Database not initialized (db is nil in main). Exiting.")
		return
	}
	defer func() {
		if db != nil {
			log.Println("Closing database connection.")
			isRaceSystemActive = false // Signal raceLoop to stop
			if raceTicker != nil {
				raceTicker.Stop()
			}
			if raceEndTimer != nil {
				raceEndTimer.Stop()
			}
			db.Close()
		}
	}()

	go raceLoop(db) // From race_logic.go

	fs := http.FileServer(http.Dir("src/web/static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// Handlers are now in their respective files but part of 'package main'
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/races", raceHandler)                               // race_handler.go
	http.HandleFunc("/login", loginHandler)                              // auth.go
	http.HandleFunc("/signup", signupHandler)                            // auth.go
	http.HandleFunc("/contact", contactHandler)                          // auth.go
	http.HandleFunc("/about-us", aboutUsHandler)                         // auth.go
	http.HandleFunc("/select-chicken/", selectChickenHandler)            // bet_handler.go
	http.HandleFunc("/calculate-winnings", calculateWinningsHandler)     // bet_handler.go
	http.HandleFunc("/place-bet", placeBetHandler)                       // bet_handler.go
	http.HandleFunc("/next-race-info", nextRaceInfoHandler)              // race_handler.go
	http.HandleFunc("/admin/trigger-race-cycle", handleTriggerRaceCycle) // race_handler.go
	http.HandleFunc("/race-update", raceUpdateHandler)                   // race_animation.go

	http.HandleFunc("/submit-contact", contactHandler)

	fmt.Printf("Server starting on http://localhost:%s\n", local_port)
	err := http.ListenAndServe(":"+local_port, nil)
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

package main

import (
	"database/sql"
	"encoding/gob"
	_ "encoding/gob"
	"fmt"
	"github.com/alexedwards/scs/v2"
	"html/template"
	"log"
	_ "math/rand"
	"net/http"
	"sync"
	"time"

	_ "github.com/alexedwards/scs/v2"
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

	sessionManager *scs.SessionManager
)

// init initializes database connection, templates, and seeds random number generator.
func init() {
	gob.Register(time.Time{})
	db = init_database() // Assuming init_database() is defined in db_utils.go or similar
	if db == nil {
		log.Fatal("Database initialization failed")
		return
	}
	// rand.Seed(time.Now().UnixNano()) // Deprecated since Go 1.20. time.Now().UnixNano() is still fine for non-crypto.
	// For Go 1.20+, rand.New(rand.NewSource(time.Now().UnixNano())) can be used if you need a specific rand instance.
	// The global rand is seeded automatically now.

	// Initialize SMTP/Contact settings
	// Ensure getEnvOrDefault is defined (e.g., in a utils.go file)
	smtpHost = getEnvOrDefault("SMTP_HOST", "smtp.gmail.com")
	smtpPort = getEnvOrDefault("SMTP_PORT", "587")
	smtpUsername = getEnvOrDefault("SMTP_USERNAME", "")
	smtpPassword = getEnvOrDefault("SMTP_PASSWORD", "")
	toEmail = getEnvOrDefault("CONTACT_EMAIL", "your-company-email@example.com")

	// --- Template Parsing ---
	// Helper function to reduce repetition
	mustParse := func(base *template.Template, name string, files ...string) *template.Template {
		var t *template.Template
		var errParse error
		if base != nil {
			t, errParse = base.Clone()
			if errParse != nil {
				log.Fatalf("Error cloning base template for %s: %v", name, errParse)
			}
		} else {
			// For baseTemplate itself, or templates without a base clone
			t = template.New(files[0]) // Use first file name as template name
		}
		parsedT, errParse := t.ParseFiles(files...)
		if errParse != nil {
			log.Fatalf("Error parsing template files for %s (%v): %v", name, files, errParse)
		}
		return parsedT
	}

	baseTemplate = mustParse(nil, "base", "src/web/templates/base.gohtml")
	homeTemplate = mustParse(baseTemplate, "home", "src/web/templates/home.gohtml")
	raceTemplate = mustParse(baseTemplate, "races", "src/web/templates/races.gohtml")
	loginTemplate = mustParse(baseTemplate, "login", "src/web/templates/login.gohtml")
	signupTemplate = mustParse(baseTemplate, "signup", "src/web/templates/signup.gohtml")
	contactTemplate = mustParse(baseTemplate, "contact", "src/web/templates/contact.gohtml")
	aboutUsTemplate = mustParse(baseTemplate, "about-us", "src/web/templates/about-us.gohtml")

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
					{{if ge .NewBalance 0.0}} {{/* Only show balance if it's not negative (e.g. insufficient funds) */}}
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
			{{ else if .CountdownStr }} {{/* Check CountdownStr first if it's more specific */}}
				Next race in:
			{{ else if .StatusMsg }} {{/* Fallback to general status message if no countdown */}}
				{{.StatusMsg}}
			{{ end }}
		</span>
		<span class="race-timer-countdown">
			{{if .CountdownStr}}
				{{.CountdownStr}}
			{{else if .IsRaceRunning}} {{/* Explicitly show "Running!" if race is running and no specific countdown*/}}
				Running!
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

	// --- Session Manager Initialization ---
	sessionManager = scs.New()
	sessionManager.Lifetime = 24 * time.Hour
	sessionManager.IdleTimeout = sessionIdleTimeout // Ensure sessionIdleTimeout is defined (e.g. in registration.go constants)
	sessionManager.Cookie.Name = "scramble_run_session"
	sessionManager.Cookie.HttpOnly = true
	sessionManager.Cookie.Persist = false
	//sessionManager.Cookie.SameSite = scs.SameSiteLaxMode
	// Determine if in production for Secure cookie
	// isProduction := os.Getenv("APP_ENV") == "production"
	// sessionManager.Cookie.Secure = isProduction
	sessionManager.Cookie.Secure = false // Set to true for production with HTTPS. For local HTTP dev, set to false.
	log.Println("Session manager initialized.")
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
			isRaceSystemActive = false
			if raceTicker != nil {
				raceTicker.Stop()
			}
			if raceEndTimer != nil {
				raceEndTimer.Stop()
			}
			// Wait a moment for raceLoop to potentially finish its current iteration cleanly
			// time.Sleep(100 * time.Millisecond) // Optional small delay
			db.Close()
		}
	}()

	go raceLoop(db)

	// Create a new ServeMux. This will be our main router.
	mux := http.NewServeMux()

	// Static files
	fs := http.FileServer(http.Dir("src/web/static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// Register handlers with our new mux
	mux.HandleFunc("/", homeHandler)
	mux.HandleFunc("/races", raceHandler)
	mux.HandleFunc("/login", loginHandler)     // From registration.go
	mux.HandleFunc("/signup", signupHandler)   // From registration.go
	mux.HandleFunc("/logout", logoutHandler)   // From registration.go (ensure it exists and handles POST)
	mux.HandleFunc("/contact", contactHandler) // Assuming this is defined
	// mux.HandleFunc("/submit-contact", contactSubmitHandler) // If /contact is for GET and /submit-contact for POST
	mux.HandleFunc("/about-us", aboutUsHandler) // Assuming this is defined

	// Betting handlers
	mux.HandleFunc("/select-chicken/", selectChickenHandler)
	mux.HandleFunc("/calculate-winnings", calculateWinningsHandler)
	mux.HandleFunc("/place-bet", placeBetHandler)

	// Race info and admin
	mux.HandleFunc("/next-race-info", nextRaceInfoHandler)
	mux.HandleFunc("/admin/trigger-race-cycle", handleTriggerRaceCycle) // Consider protecting this admin route
	mux.HandleFunc("/race-update", raceUpdateHandler)

	// If /submit-contact is the POST target for the contact form handled by contactHandler:
	// mux.HandleFunc("/submit-contact", contactHandler) // This is fine if contactHandler checks r.Method

	// --- Important: Apply middleware ---
	// sessionManager.LoadAndSave will wrap our entire mux.
	// All requests will go through this middleware first.
	handlerWithSession := sessionManager.LoadAndSave(mux)

	// TODO: Add other middleware here if needed, e.g., CSRF protection, logging, etc.
	// Example: handlerWithSessionAndCSRF := nosurf.New(handlerWithSession)

	fmt.Printf("Server starting on http://localhost:%s\n", local_port)
	// Use the handler wrapped with middleware
	err := http.ListenAndServe(":"+local_port, handlerWithSession)
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	_ "github.com/mattn/go-sqlite3"
)

const local_port string = "6969"

var (
	db                  *sql.DB
	baseTemplate        *template.Template
	homeTemplate        *template.Template
	loginTemplate       *template.Template
	signupTemplate      *template.Template
	betResponseTemplate *template.Template // Added for placeBetHandler response

	// Global list of chickens, consistent with what's used elsewhere
	availableChickens = []Chicken{
		{ID: 1, Name: "Henrietta", Color: "red", Odds: 2.5, Lane: 10, Progress: 0},
		{ID: 2, Name: "Cluck Norris", Color: "blue", Odds: 3.0, Lane: 50, Progress: 0},
		{ID: 3, Name: "Foghorn", Color: "green", Odds: 4.0, Lane: 90, Progress: 0},
	}
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
	ID    int
	Name  string
	Email string
	Age   int
	// Balance float64 // Consider adding Balance here if you fetch full user data often
}

type PageData struct {
	Title             string
	UserData          User
	Races             []RaceInfo
	Chickens          []Chicken  // For betting panel
	ActiveRace        ActiveRace // Current race in progress
	NextRaceTime      string     // Time until next race
	PotentialWinnings float64    // Calculated winnings
	Message           string     // For form feedback
	Success           bool       // For form feedback
	// UserBalance float64 // Add this if you want to display balance on the page from PageData
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

type rowQuerier interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

func init() {
	var err error

	db = init_database()
	if db == nil {
		log.Fatal("Database initialization failed")
		return
	}

	baseTemplate, err = template.ParseFiles("src/web/templates/base.gohtml")
	if err != nil {
		log.Fatalf("Error parsing base template: %v", err)
	}

	homeTemplate, err = template.Must(baseTemplate.Clone()).ParseFiles("src/web/templates/home.gohtml")
	if err != nil {
		log.Fatalf("Error parsing home template: %v", err)
	}

	loginTemplate, err = template.Must(baseTemplate.Clone()).ParseFiles("src/web/templates/login.gohtml")
	if err != nil {
		log.Fatalf("Error parsing login template: %v", err)
	}

	signupTemplate, err = template.Must(baseTemplate.Clone()).ParseFiles("src/web/templates/signup.gohtml")
	if err != nil {
		log.Fatalf("Error parsing signup template: %v", err)
	}

	betResponseTemplate = template.Must(template.New("betResponse").Parse(`
		<div class="bet-response" id="bet-response">
			{{if .Success}}
				<div class="alert alert-success">
					<p>{{.Message}}</p>
					<p>Bet placed: {{printf "%.2f" .BetAmount}} credits on {{.ChickenName}}</p>
					<p>New balance: {{printf "%.2f" .NewBalance}} credits</p>
				</div>
			{{else}}
				<div class="alert alert-danger"> <!-- Changed to alert-danger for better styling -->
					<p>{{.Message}}</p>
				</div>
			{{end}}
		</div>
	`))
	log.Println("Templates loaded successfully")
}

func init_database() *sql.DB {
	db, err := sql.Open("sqlite3", "src/internal/database/scramble.db")
	if err != nil {
		log.Printf("Failed to connect to the database: %v", err)
		return nil
	}

	var tableCount int
	// Check if a core table like 'users' exists to gauge if DB is initialized
	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='users';").Scan(&tableCount)
	if err != nil {
		log.Printf("Failed to check for 'users' table: %v", err)
		// Don't return nil here, as we might want to proceed with init if the error is "no such table"
		// or if the count is 0
	}

	shouldInitialize := false
	if tableCount == 0 {
		shouldInitialize = true
	} else {
		// If users table exists, check if 'balance' column exists. This is a simple migration check.
		var balanceColExists int
		// This query is specific to SQLite to check for column existence
		err = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='balance'").Scan(&balanceColExists)
		if err != nil {
			log.Printf("Failed to check for 'balance' column in 'users' table: %v", err)
			// If this check fails, it's safer to assume we might need to run init again,
			// or have a more sophisticated migration system. For now, let's assume init is needed.
			shouldInitialize = true
		} else if balanceColExists == 0 {
			log.Println("'balance' column not found in 'users' table. Database might need re-initialization or migration.")
			// Depending on your init_database.sql, this might be okay if it uses ALTER TABLE ADD COLUMN.
			// For simplicity here, we'll assume init_database.sql is idempotent or handles this.
			shouldInitialize = true
		}
	}


	if shouldInitialize {
		log.Println("Attempting to initialize database from SQL file...")
		sql_file, err_read := os.ReadFile("src/internal/database/init_database.sql")
		if err_read != nil {
			log.Printf("Failed to read SQL initialization file: %v", err_read)
			// Close DB if we can't initialize, as it might be in an inconsistent state
			db.Close()
			return nil
		}

		_, err_exec := db.Exec(string(sql_file))
		if err_exec != nil {
			log.Printf("Failed to initialize the database: %v", err_exec)
			db.Close()
			return nil
		}
		fmt.Println("Database initialized/verified successfully")
	} else {
		fmt.Println("Database already initialized or structure seems up-to-date.")
	}

	return db
}

func getActiveRaceID(db rowQuerier) (int, error) {
	var raceID int
	// Select the race with the latest date that does not have a winner.
	// Order by date DESC ensures we prefer later scheduled races.
	// Order by id DESC is a tie-breaker for races on the same date.
	err := db.QueryRow("SELECT id FROM races WHERE winner IS NULL OR winner = '' ORDER BY date DESC, id DESC LIMIT 1").Scan(&raceID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("no active race available for betting at the moment")
		}
		return 0, fmt.Errorf("error fetching active race ID: %w", err)
	}
	return raceID, nil
}

func getPendingBetStatusID(db rowQuerier) (int, error) {
	var statusID int
	err := db.QueryRow("SELECT id FROM bet_statuses WHERE status_name = 'Pending'").Scan(&statusID)
	if err != nil {
		if err == sql.ErrNoRows {
			// This is a critical setup error if 'Pending' status doesn't exist.
			return 0, fmt.Errorf("bet status 'Pending' not found in database; please ensure bet_statuses table is initialized correctly")
		}
		return 0, fmt.Errorf("error fetching 'Pending' bet status ID: %w", err)
	}
	return statusID, nil
}

func get_races(db *sql.DB) []RaceInfo {
	rows, err := db.Query("SELECT id, name, date, winner FROM races;")
	if err != nil {
		log.Printf("get_races: Failed to query races: %v", err)
		return nil
	}
	defer rows.Close()

	var races []RaceInfo
	for rows.Next() {
		var race RaceInfo
		var dateStr string // Scan date as string first
		err = rows.Scan(&race.Id, &race.Name, &dateStr, &race.Winner)
		if err != nil {
			log.Printf("get_races: Failed to scan row: %v", err)
			continue
		}

		// Attempt to parse with RFC3339 first, as seen in logs
		parsedTime, errParse := time.Parse(time.RFC3339, dateStr)
		if errParse != nil {
			// Fallback to other common SQLite datetime formats if RFC3339 fails
			// This order might need adjustment based on how SQLite stores dates by default
			// if `TIMESTAMP DEFAULT CURRENT_TIMESTAMP` is used without explicit formatting.
			// SQLite often stores in "YYYY-MM-DD HH:MM:SS"
			layout1 := "2006-01-02 15:04:05"
			parsedTime, errParse = time.Parse(layout1, dateStr)
			if errParse != nil {
				layout2 := "2006-01-02" // If it's just a date
                parsedTime, errParse = time.Parse(layout2, dateStr)
                if errParse != nil {
					log.Printf("get_races: Failed to parse date string '%s' for race ID %d using multiple formats. Last error: %v", dateStr, race.Id, errParse)
					// Optionally set a zero time or skip race
					race.Date = time.Time{} // Set to zero time if parsing fails
                } else {
					race.Date = parsedTime
				}
			} else {
				race.Date = parsedTime
			}
		} else {
			race.Date = parsedTime
		}

		// log.Printf("Scanned race: ID=%d, Name=%s, Winner=%s, Date=%v", race.Id, race.Name, race.Winner, race.Date)
		races = append(races, race)
	}
	if err := rows.Err(); err != nil {
		log.Printf("get_races: Error iterating through rows: %v", err)
		return nil
	}
	return races
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: Get userID from session to fetch actual user data and balance
	// userID := getUserIDFromSession(r) // Placeholder for session logic
	// var currentUser User
	// var userBalance float64
	// if userID != 0 {
	//    err := db.QueryRow("SELECT id, name, email, balance FROM users WHERE id = ?", userID).Scan(¤tUser.ID, ¤tUser.Name, ¤tUser.Email, &userBalance)
	//    if err != nil {
	//        log.Printf("homeHandler: Error fetching user data: %v", err)
	//        // Handle error, maybe redirect to login or show guest view
	//    }
	// } else {
	//    // Guest user
	//    currentUser.Name = "Guest"
	// }

	data := PageData{
		Title: "Scramble Run",
		UserData: User{ // This should be populated for the logged-in user
			Name: "test_user", // Placeholder
			Age:  99,          // Placeholder
		},
		Races:    get_races(db),
		Chickens: availableChickens, // Use global list
		ActiveRace: ActiveRace{
			Chickens: availableChickens, // Use global list
		},
		NextRaceTime:      "5 minutes", // Placeholder
		PotentialWinnings: 0.0,       // Will be updated by HTMX
		// UserBalance: userBalance, // Pass actual user balance to the template
	}

	err := homeTemplate.ExecuteTemplate(w, "base.gohtml", data)
	if err != nil {
		log.Printf("homeHandler: Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title: "Login - Scramble Run",
	}

	if r.Method == http.MethodPost {
		err := r.ParseForm()
		if err != nil {
			log.Printf("loginHandler: Error parsing form: %v", err)
			data.Message = "Error processing form"; data.Success = false
			_ = loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		email := r.FormValue("email")
		password := r.FormValue("password")

		if email == "" || password == "" {
			data.Message = "Email and password are required"; data.Success = false
			_ = loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		var storedPasswordHash string
		var userID int
		var userName string
		// var userBalance float64 // If you want to store balance in session

		err = db.QueryRow("SELECT id, name, password_hash FROM users WHERE email = ?", email).Scan(&userID, &userName, &storedPasswordHash)
		// err = db.QueryRow("SELECT id, name, password_hash, balance FROM users WHERE email = ?", email).Scan(&userID, &userName, &storedPasswordHash, &userBalance) // If getting balance
		if err != nil {
			if err == sql.ErrNoRows {
				data.Message = "Invalid email or password"
			} else {
				log.Printf("loginHandler: Database error: %v", err)
				data.Message = "An error occurred. Please try again."
			}
			data.Success = false
			_ = loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		err = bcrypt.CompareHashAndPassword([]byte(storedPasswordHash), []byte(password))
		if err != nil {
			data.Message = "Invalid email or password" // Keep error message generic
			data.Success = false
			_ = loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		// Login successful
		// TODO: Implement actual session management (e.g., using gorilla/sessions or similar)
		// Example:
		// session, _ := store.Get(r, "scramble-session")
		// session.Values["userID"] = userID
		// session.Values["userName"] = userName
		// session.Values["userBalance"] = userBalance // Store balance if needed frequently
		// err = session.Save(r, w)
		// if err != nil {
		//    log.Printf("loginHandler: Error saving session: %v", err)
		//    http.Error(w, "Failed to save session", http.StatusInternalServerError)
		//    return
		// }
		log.Printf("User %s (ID: %d) logged in successfully.", userName, userID)

		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	err := loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
	if err != nil {
		log.Printf("loginHandler: Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func signupHandler(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title: "Signup - Scramble Run",
	}

	if r.Method == http.MethodPost {
		err := r.ParseForm()
		if err != nil {
			log.Printf("signupHandler: Error parsing form: %v", err)
			data.Message = "Error processing form"; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		name := r.FormValue("name")
		email := r.FormValue("email")
		password := r.FormValue("password")
		confirmPassword := r.FormValue("confirm_password")

		if name == "" || email == "" || password == "" {
			data.Message = "All fields are required"; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}
		if !strings.Contains(email, "@") { // Basic email validation
			data.Message = "Invalid email format"; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}
		if len(password) < 6 { // Basic password length check
			data.Message = "Password must be at least 6 characters"; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}
		if password != confirmPassword {
			data.Message = "Passwords do not match"; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", email).Scan(&count)
		if err != nil {
			log.Printf("signupHandler: Database error checking email: %v", err)
			data.Message = "An error occurred. Please try again."; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}
		if count > 0 {
			data.Message = "Email already in use"; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("signupHandler: Error hashing password: %v", err)
			data.Message = "An error occurred. Please try again."; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		// Assumes users table has a 'balance' column with a DEFAULT value (e.g., 1000.0)
		// set in init_database.sql. If not, insert explicitly.
		// For example, `DEFAULT 100.0` in `CREATE TABLE users (... balance REAL DEFAULT 100.0 ...)`
		_, err = db.Exec("INSERT INTO users (name, email, password_hash) VALUES (?, ?, ?)",
			name, email, string(hashedPassword))
		if err != nil {
			log.Printf("signupHandler: Error inserting user: %v", err)
			data.Message = "An error occurred. Please try again."; data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		data.Message = "Registration successful! You can now log in."
		data.Success = true
		// It's better to redirect to login after successful signup, or show the message on the login page.
		// For now, rendering login template with the success message.
		_ = loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
		return
	}

	err := signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
	if err != nil {
		log.Printf("signupHandler: Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func selectChickenHandler(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 3 {
		http.Error(w, "Invalid URL: Missing chicken ID", http.StatusBadRequest)
		return
	}

	chickenIDStr := pathParts[len(pathParts)-1]
	chickenID, err := strconv.Atoi(chickenIDStr)
	if err != nil {
		http.Error(w, "Invalid chicken ID format", http.StatusBadRequest)
		return
	}

	var selectedChicken Chicken
	found := false
	for _, chicken := range availableChickens {
		if chicken.ID == chickenID {
			selectedChicken = chicken
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "Chicken not found", http.StatusNotFound)
		return
	}

	betAmount := 10.0 // Default bet amount
	betAmountStr := r.URL.Query().Get("betAmount")
	if betAmountStr != "" {
		parsedAmount, parseErr := strconv.ParseFloat(betAmountStr, 64)
		if parseErr == nil && parsedAmount > 0 {
			betAmount = parsedAmount
		}
	}

	potentialWinnings := betAmount * selectedChicken.Odds

	w.Header().Set("Content-Type", "text/html")
	// Define the template locally for this handler for clarity or use a pre-parsed one
	tmpl := template.Must(template.New("winningsCalc").Parse(`
        <div class="winnings-display" id="winnings-calc">
            <p>Potential Win:</p>
            <span class="winnings-amount">{{printf "%.2f" .Amount}} Credits</span>
            <input type="hidden" name="selectedChicken" value="{{.ChickenID}}" />
        </div>
    `))

	winningsData := WinningsCalc{
		Amount:    potentialWinnings,
		ChickenID: chickenID,
	}

	err = tmpl.Execute(w, winningsData)
	if err != nil {
		log.Printf("selectChickenHandler: Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func calculateWinningsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		log.Printf("calculateWinningsHandler: Failed to parse form: %v", err)
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}

	betAmountStr := r.Form.Get("betAmount")
	betAmount, err := strconv.ParseFloat(betAmountStr, 64)
	if err != nil || betAmount <= 0 {
		http.Error(w, "Invalid bet amount", http.StatusBadRequest)
		return
	}

	chickenIDStr := r.Form.Get("selectedChicken")
	chickenID, err := strconv.Atoi(chickenIDStr)
	if err != nil {
		http.Error(w, "Invalid chicken ID", http.StatusBadRequest)
		return
	}

	var selectedChickenOdds float64
	found := false
	for _, chicken := range availableChickens {
		if chicken.ID == chickenID {
			selectedChickenOdds = chicken.Odds
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "Chicken not found", http.StatusNotFound)
		return
	}

	winningsData := WinningsCalc{
		Amount:    betAmount * selectedChickenOdds,
		ChickenID: chickenID,
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl := template.Must(template.New("winningsCalcResponse").Parse(`
        <div class="winnings-display" id="winnings-calc">
            <p>Potential Win:</p>
            <span class="winnings-amount">{{printf "%.2f" .Amount}} Credits</span>
            <input type="hidden" name="selectedChicken" value="{{.ChickenID}}" />
        </div>
    `))

	err = tmpl.Execute(w, winningsData)
	if err != nil {
		log.Printf("calculateWinningsHandler: Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func placeBetHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	if r.Method != http.MethodPost {
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Method not allowed"})
		return
	}

	currentUserID := 1 // <<< --- !!! PLACEHOLDER: Replace with actual User ID from session !!! --- >>>

	err := r.ParseForm()
	if err != nil {
		log.Printf("placeBetHandler: Failed to parse form: %v", err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Error processing request."})
		return
	}

	log.Println("--- placeBetHandler Received Form Data ---")
	for key, values := range r.Form {
		for _, value := range values {
			log.Printf("Form Key: [%s], Value: [%s]\n", key, value)
		}
	}
	log.Println("----------------------------------------")

	betAmountStr := r.FormValue("betAmount")
	log.Printf("placeBetHandler: Raw betAmountStr from form: '%s'", betAmountStr)
	betAmount, err := strconv.ParseFloat(betAmountStr, 64)
	if err != nil || betAmount <= 0 {
		log.Printf("placeBetHandler: Invalid bet amount. String: '%s', Parsed: %f, Error: %v", betAmountStr, betAmount, err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Invalid bet amount. Must be a positive number."})
		return
	}

	chickenIDStr := r.FormValue("selectedChicken")
	log.Printf("placeBetHandler: Raw chickenIDStr from form: '%s'", chickenIDStr)
	if chickenIDStr == "" {
		log.Println("placeBetHandler: 'selectedChicken' form value is empty.")
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "No chicken selected. Please select a chicken first."})
		return
	}
	chickenID, err := strconv.Atoi(chickenIDStr)
	if err != nil {
		log.Printf("placeBetHandler: Failed to convert 'selectedChicken' value '%s' to an integer: %v", chickenIDStr, err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Invalid chicken selection data. Expected a numeric ID."})
		return
	}
	log.Printf("placeBetHandler: Parsed chickenID: %d", chickenID)

	var selectedChicken Chicken
	foundChicken := false
	for _, ch := range availableChickens {
		if ch.ID == chickenID {
			selectedChicken = ch
			foundChicken = true
			break
		}
	}
	if !foundChicken {
		log.Printf("placeBetHandler: Chicken with ID %d not found in availableChickens list.", chickenID)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: fmt.Sprintf("The selected chicken (ID: %d) is not available for betting.", chickenID)})
		return
	}
	log.Printf("placeBetHandler: Successfully found chicken: %s (ID: %d)", selectedChicken.Name, selectedChicken.ID)

	tx, err := db.Begin()
	if err != nil {
		log.Printf("placeBetHandler: Failed to begin transaction: %v", err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Database error. Please try again later."})
		return
	}
	committed := false
	defer func() {
		if !committed {
			errRollback := tx.Rollback()
			if errRollback != nil {
				log.Printf("placeBetHandler: Error rolling back transaction: %v", errRollback)
			} else {
				log.Println("placeBetHandler: Transaction rolled back due to error.")
			}
		}
	}()

	var currentUserBalance float64
	err = tx.QueryRow("SELECT balance FROM users WHERE id = ?", currentUserID).Scan(&currentUserBalance)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("placeBetHandler: User ID %d not found for betting.", currentUserID)
			_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "User not found."})
		} else {
			log.Printf("placeBetHandler: Error fetching user balance: %v", err)
			_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Database error while fetching balance."})
		}
		return
	}

	if currentUserBalance < betAmount {
		_ = betResponseTemplate.Execute(w, BetResponse{
			Success:     false,
			Message:     fmt.Sprintf("Insufficient funds. Your balance is %.2f credits.", currentUserBalance),
			NewBalance:  currentUserBalance, // Show current balance even on failure
			BetAmount:   betAmount,
			ChickenName: selectedChicken.Name,
		})
		return
	}

	// Get active race ID
	activeRaceID, err := getActiveRaceID(tx) // Use the transaction
	if err != nil {
		log.Printf("placeBetHandler: Could not determine active race: %v", err)
		msg := "Failed to determine active race for betting. Please try again later."
		if strings.Contains(err.Error(), "no active race available") {
			msg = "No races are currently open for betting. Please check back later."
		}
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: msg})
		return
	}
	log.Printf("placeBetHandler: Active Race ID for bet: %d", activeRaceID)


	// Get 'Pending' status ID
	pendingStatusID, err := getPendingBetStatusID(tx) // Use the transaction
	if err != nil {
		log.Printf("placeBetHandler: Could not determine pending bet status: %v", err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "System error: Bet status configuration issue."})
		return
	}
	log.Printf("placeBetHandler: Pending Bet Status ID: %d", pendingStatusID)


	newBalance := currentUserBalance - betAmount
	_, err = tx.Exec("UPDATE users SET balance = ? WHERE id = ?", newBalance, currentUserID)
	if err != nil {
		log.Printf("placeBetHandler: Error updating user balance: %v", err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Failed to update balance."})
		return
	}

	// Insert the bet with race_id and bet_status_id
	_, err = tx.Exec("INSERT INTO bets (user_id, race_id, chicken_id, bet_amount, bet_status_id) VALUES (?, ?, ?, ?, ?)",
		currentUserID, activeRaceID, chickenID, betAmount, pendingStatusID)
	if err != nil {
		log.Printf("placeBetHandler: Error inserting bet: %v", err) // This was the original error point
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Failed to record bet."})
		return
	}

	err = tx.Commit()
	if err != nil {
		log.Printf("placeBetHandler: Error committing transaction: %v", err)
		_ = betResponseTemplate.Execute(w, BetResponse{Success: false, Message: "Failed to finalize bet."})
		return
	}
	committed = true
	log.Printf("placeBetHandler: Bet successfully placed for user %d on chicken %d (Race %d) for amount %.2f. New balance: %.2f", currentUserID, chickenID, activeRaceID, betAmount, newBalance)

	response := BetResponse{
		Success:     true,
		Message:     "Bet placed successfully!",
		NewBalance:  newBalance,
		BetAmount:   betAmount,
		ChickenName: selectedChicken.Name,
	}

	err = betResponseTemplate.Execute(w, response)
	if err != nil {
		log.Printf("placeBetHandler: Failed to render success response: %v", err)
		// Bet was placed, but response failed. Send plain error to client.
		http.Error(w, "Internal Server Error (bet placed, but response failed to render)", http.StatusInternalServerError)
	}
}

func main() {
	if db == nil {
		log.Fatal("Database not initialized. Exiting.") // db should be initialized in init()
		return
	}
	defer db.Close()

	fs := http.FileServer(http.Dir("src/web/static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/signup", signupHandler)
	http.HandleFunc("/select-chicken/", selectChickenHandler) // Note trailing slash if IDs are path segments
	http.HandleFunc("/calculate-winnings", calculateWinningsHandler)
	http.HandleFunc("/place-bet", placeBetHandler)

	// TODO: Add a /logout handler
	// http.HandleFunc("/logout", logoutHandler)

	fmt.Printf("Server starting on http://localhost:%s\n", local_port)
	err := http.ListenAndServe(":"+local_port, nil)
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
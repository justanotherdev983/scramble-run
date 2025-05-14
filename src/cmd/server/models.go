package main

import (
	"database/sql"
	"time"
)

// rowQuerier interface for functions that can query a single row
type rowQuerier interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

// RaceInfo stores details about a single race.
type RaceInfo struct {
	Id              int
	Name            string
	Winner          string        // Name of the winning chicken
	WinnerChickenID sql.NullInt64 // ID of the winning chicken from DB (can be NULL)
	ChickenNames    []string      // Names of chickens participating (can be dynamic later)
	Date            time.Time     // Scheduled Start Time
	Status          string        // 'Scheduled', 'Running', 'Finished'
}

// Chicken represents a participant in a race.
type Chicken struct {
	ID       int
	Name     string
	Color    string
	Odds     float64
	Lane     int
	Progress float64
}

// ActiveRace holds information about the chickens in the currently active race (for display).
type ActiveRace struct {
	Chickens []Chicken
}

// User represents a user of the application.
type User struct {
	ID    int
	Name  string
	Email string
	Age   int
	// Balance float64 // Consider adding Balance here if you fetch full user data often
}

// PageData is used to pass data to HTML templates.
type PageData struct {
	Title       string
	UserData    User
	UserBalance float64
	Races       []RaceInfo // This is for the history list
	Chickens    []Chicken  // This is availableChickens for betting selection
	ActiveRace  ActiveRace // This is for displaying chickens on the track

	InitialNextRaceTime    string
	InitialStatusMessage   string
	InitialRaceName        string
	IsBettingInitiallyOpen bool
	CurrentRaceDisplay     *RaceInfo // Details of the current/last race from race manager

	PotentialWinnings float64
	Message           string // For bet responses, etc. (Good to have)
	Success           bool   // For bet responses, etc. (Good to have)

	RaceStatus string // Overall status for the track display (Scheduled, Running, Finished, NoRace)

	// ADD THESE FIELDS:
	RaceFinished bool
	WinnerID     int // Or string, if your Chicken IDs are strings. Must match type of Chicken.ID

	IsLoggedIn      bool   // Useful for base template
	CurrentUserName string // Useful for base template
	CSRFToken       string // For CSRF protection
}

// WinningsCalc is used for calculating and displaying potential winnings.
type WinningsCalc struct {
	Amount    float64
	ChickenID int
}

// BetResponse is used for the HTMX response from placeBetHandler.
type BetResponse struct {
	Success     bool
	Message     string
	NewBalance  float64
	BetAmount   float64
	ChickenName string
}

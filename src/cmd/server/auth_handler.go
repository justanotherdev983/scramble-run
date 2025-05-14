package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Environment variables for email configuration
var (
	smtpHost     = getEnvOrDefault("SMTP_HOST", "smtp.gmail.com")
	smtpPort     = getEnvOrDefault("SMTP_PORT", "587")
	smtpUsername = getEnvOrDefault("SMTP_USERNAME", "")
	smtpPassword = getEnvOrDefault("SMTP_PASSWORD", "")
	toEmail      = getEnvOrDefault("CONTACT_EMAIL", "your-company-email@example.com")
)

// ContactMessage represents a contact form submission
type ContactMessage struct {
	Topic     string
	Email     string
	Message   string
	CreatedAt time.Time
	IPAddress string
}

// getEnvOrDefault returns the environment variable or a default value if not set
func getEnvOrDefault(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// loginHandler handles user login requests.
func loginHandler(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title: "Login - Scramble Run",
	}

	if r.Method == http.MethodPost {
		err := r.ParseForm()
		if err != nil {
			log.Printf("loginHandler: Error parsing form: %v", err)
			data.Message = "Error processing form"
			data.Success = false
			_ = loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		email := r.FormValue("email")
		password := r.FormValue("password")

		if email == "" || password == "" {
			data.Message = "Email and password are required"
			data.Success = false
			_ = loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		var storedPasswordHash string
		var userID int
		var userName string

		err = db.QueryRow("SELECT id, name, password_hash FROM users WHERE email = ?", email).Scan(&userID, &userName, &storedPasswordHash)
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
			data.Message = "Invalid email or password"
			data.Success = false
			_ = loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		log.Printf("User %s (ID: %d) logged in successfully.", userName, userID)
		// TODO: Implement actual session management
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	err := loginTemplate.ExecuteTemplate(w, "base.gohtml", data)
	if err != nil {
		log.Printf("loginHandler: Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// signupHandler handles new user registration requests.
func signupHandler(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title: "Signup - Scramble Run",
	}

	if r.Method == http.MethodPost {
		err := r.ParseForm()
		if err != nil {
			log.Printf("signupHandler: Error parsing form: %v", err)
			data.Message = "Error processing form"
			data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		name := r.FormValue("name")
		email := r.FormValue("email")
		password := r.FormValue("password")
		confirmPassword := r.FormValue("confirm_password")

		if name == "" || email == "" || password == "" {
			data.Message = "All fields are required"
			data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}
		if !strings.Contains(email, "@") {
			data.Message = "Invalid email format"
			data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}
		if len(password) < 6 {
			data.Message = "Password must be at least 6 characters"
			data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}
		if password != confirmPassword {
			data.Message = "Passwords do not match"
			data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", email).Scan(&count)
		if err != nil {
			log.Printf("signupHandler: Database error checking email: %v", err)
			data.Message = "An error occurred. Please try again."
			data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}
		if count > 0 {
			data.Message = "Email already in use"
			data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("signupHandler: Error hashing password: %v", err)
			data.Message = "An error occurred. Please try again."
			data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		_, err = db.Exec("INSERT INTO users (name, email, password_hash) VALUES (?, ?, ?)",
			name, email, string(hashedPassword))
		if err != nil {
			log.Printf("signupHandler: Error inserting user: %v", err)
			data.Message = "An error occurred. Please try again."
			data.Success = false
			_ = signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		data.Message = "Registration successful! You can now log in."
		data.Success = true
		_ = loginTemplate.ExecuteTemplate(w, "base.gohtml", data) // Show message on login page
		return
	}

	err := signupTemplate.ExecuteTemplate(w, "base.gohtml", data)
	if err != nil {
		log.Printf("signupHandler: Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func aboutUsHandler(w http.ResponseWriter, r *http.Request) {

	data := PageData{
		Title: "Signup - Scramble Run",
	}

	err := aboutUsTemplate.ExecuteTemplate(w, "base.gohtml", data)
	if err != nil {
		log.Printf("signupHandler: Template execution error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// contactHandler handles contact form submissions.
func contactHandler(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title: "Contact Us - Scramble Run",
	}

	if r.Method == http.MethodGet {
		err := contactTemplate.ExecuteTemplate(w, "base.gohtml", data)
		if err != nil {
			log.Printf("contactHandler: Template execution error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	if r.Method == http.MethodPost {
		// Parse form data
		err := r.ParseForm()
		if err != nil {
			log.Printf("contactHandler: Error parsing form: %v", err)
			data.Message = "Error processing form"
			data.Success = false
			_ = contactTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		// Extract form values
		topic := r.FormValue("topic")
		email := r.FormValue("email")
		message := r.FormValue("message")

		// Validate form data
		if topic == "" || email == "" || message == "" {
			data.Message = "All fields are required"
			data.Success = false
			_ = contactTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		if !strings.Contains(email, "@") {
			data.Message = "Invalid email format"
			data.Success = false
			_ = contactTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		// Get the client's IP address
		ipAddress := getIPAddress(r)

		// Create contact message object
		contactMsg := ContactMessage{
			Topic:     topic,
			Email:     email,
			Message:   message,
			CreatedAt: time.Now(),
			IPAddress: ipAddress,
		}

		// Save to database for record keeping
		err = saveContactMessage(contactMsg)
		if err != nil {
			log.Printf("contactHandler: Error saving contact message: %v", err)
			// Continue with email sending even if DB save fails
		}

		// Send email notification
		err = sendContactEmail(contactMsg)
		if err != nil {
			log.Printf("contactHandler: Error sending email: %v", err)
			data.Message = "There was a problem sending your message. Please try again later."
			data.Success = false
			_ = contactTemplate.ExecuteTemplate(w, "base.gohtml", data)
			return
		}

		// Return success to user
		data.Message = "Thank you! Your message has been sent successfully. We'll be in touch soon."
		data.Success = true
		_ = contactTemplate.ExecuteTemplate(w, "base.gohtml", data)
		return
	}

	// Method not allowed for methods other than GET or POST
	http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
}

// saveContactMessage saves a contact message to the database
func saveContactMessage(msg ContactMessage) error {
	_, err := db.Exec(
		"INSERT INTO contact_messages (topic, email, message, created_at, ip_address) VALUES (?, ?, ?, ?, ?)",
		msg.Topic, msg.Email, msg.Message, msg.CreatedAt, msg.IPAddress,
	)
	return err
}

// sendContactEmail sends an email with the contact form submission
func sendContactEmail(msg ContactMessage) error {
	// Skip sending emails in development if credentials aren't set
	if smtpUsername == "" || smtpPassword == "" {
		log.Println("Warning: SMTP credentials not set. Email sending skipped.")
		return nil
	}

	// Set up authentication
	auth := smtp.PlainAuth("", smtpUsername, smtpPassword, smtpHost)

	// Compose email
	to := []string{toEmail}
	subject := fmt.Sprintf("Contact Form: %s", msg.Topic)
	body := fmt.Sprintf(
		"Contact Form Submission\n\n"+
			"Topic: %s\n"+
			"From: %s\n"+
			"Date: %s\n"+
			"IP: %s\n\n"+
			"Message:\n%s\n",
		msg.Topic, msg.Email, msg.CreatedAt.Format(time.RFC1123),
		msg.IPAddress, msg.Message,
	)

	// Assemble email with headers
	emailContent := fmt.Sprintf(
		"To: %s\r\n"+
			"From: %s\r\n"+
			"Subject: %s\r\n"+
			"MIME-Version: 1.0\r\n"+
			"Content-Type: text/plain; charset=utf-8\r\n"+
			"\r\n"+
			"%s",
		toEmail, smtpUsername, subject, body,
	)

	// Send email
	err := smtp.SendMail(
		fmt.Sprintf("%s:%s", smtpHost, smtpPort),
		auth,
		smtpUsername,
		to,
		[]byte(emailContent),
	)
	return err
}

// getIPAddress gets the real IP address from the request
func getIPAddress(r *http.Request) string {
	// Check for proxy headers first
	for _, h := range []string{"X-Forwarded-For", "X-Real-Ip"} {
		addresses := strings.Split(r.Header.Get(h), ",")
		if len(addresses) > 0 && addresses[0] != "" {
			return strings.TrimSpace(addresses[0])
		}
	}

	// Extract IP from RemoteAddr
	ip := r.RemoteAddr
	colon := strings.LastIndex(ip, ":")
	if colon != -1 {
		ip = ip[:colon]
	}

	return ip
}

// initContactDatabase creates the contact_messages table if it doesn't exist
func initContactDatabase() error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS contact_messages (
			id INTEGER PRIMARY KEY AUTO_INCREMENT,
			topic VARCHAR(255) NOT NULL,
			email VARCHAR(255) NOT NULL,
			message TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL,
			ip_address VARCHAR(45) NOT NULL
		)
	`)
	return err
}

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"

	_ "github.com/alexedwards/scs/v2" // Corrected: Import SCS for direct use
	"golang.org/x/crypto/bcrypt"
	// "your_project/internal/middleware" // Hypothetical for CSRF or MaxBytesReader
)

// --- Configuration Constants ---
const (
	minPasswordLength      = 8
	bcryptCost             = 12
	dbTimeout              = 3 * time.Second
	hashTimeout            = 5 * time.Second // For operations including password hashing
	sessionUserIDKey       = "userID"
	sessionUserNameKey     = "userName"
	sessionAuthTimeKey     = "authenticatedAt"
	sessionIdleTimeout     = 30 * time.Minute // Example: log out after 30 mins of inactivity
	sessionAbsoluteTimeout = 12 * time.Hour   // Example: force re-login after 12 hours regardless of activity
	maxFormMemory          = 1 * 1024 * 1024  // 1MB for form parsing in memory, adjust as needed
)

// --- Utility Functions ---

func isValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func renderTemplateWithStatus(w http.ResponseWriter, r *http.Request, status int, tmpl *template.Template, templateName string, data PageData) {
	if sessionManager == nil {
		log.Println("renderTemplateWithStatus: CRITICAL: sessionManager is nil. Ensure it's initialized.")
		// Fallback or panic, as this indicates a setup error
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	data.IsLoggedIn = sessionManager.Exists(r.Context(), sessionUserIDKey)
	if data.IsLoggedIn {
		data.CurrentUserName = sessionManager.GetString(r.Context(), sessionUserNameKey)
	}
	// TODO: Populate CSRF token here if using nosurf or similar
	// data.CSRFToken = nosurf.Token(r)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	err := tmpl.ExecuteTemplate(w, templateName, data)
	if err != nil {
		log.Printf("renderTemplate: Error executing template '%s': %v", templateName, err)
	}
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if sessionManager == nil {
		log.Println("loginHandler: CRITICAL: sessionManager is nil.")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if sessionManager.Exists(r.Context(), sessionUserIDKey) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	data := PageData{Title: "Login - Scramble Run"}

	if r.Method == http.MethodGet {
		renderTemplateWithStatus(w, r, http.StatusOK, loginTemplate, "base.gohtml", data)
		return
	}

	if r.Method == http.MethodPost {
		// TODO: Validate CSRF token here.

		// Limit request body size before parsing the form.
		// This is a good place for it if not handled by a global middleware.
		r.Body = http.MaxBytesReader(w, r.Body, maxFormMemory)
		if err := r.ParseForm(); err != nil {
			if _, ok := err.(*http.MaxBytesError); ok {
				log.Printf("loginHandler: Form too large: %v", err)
				data.Message = "The submitted form is too large. Please try again."
				renderTemplateWithStatus(w, r, http.StatusRequestEntityTooLarge, loginTemplate, "base.gohtml", data)
				return
			}
			log.Printf("loginHandler: Error parsing form: %v", err)
			data.Message = "Error processing form. Please try again."
			renderTemplateWithStatus(w, r, http.StatusBadRequest, loginTemplate, "base.gohtml", data)
			return
		}

		email := normalizeEmail(r.FormValue("email"))
		password := r.FormValue("password")

		if email == "" || password == "" {
			data.Message = "Email and password are required."
			renderTemplateWithStatus(w, r, http.StatusBadRequest, loginTemplate, "base.gohtml", data)
			return
		}
		if !isValidEmail(email) {
			data.Message = "Invalid email format."
			renderTemplateWithStatus(w, r, http.StatusBadRequest, loginTemplate, "base.gohtml", data)
			return
		}
		if len(password) > 256 {
			data.Message = "Password is too long."
			renderTemplateWithStatus(w, r, http.StatusBadRequest, loginTemplate, "base.gohtml", data)
			return
		}

		// TODO: Implement rate limiting for login attempts.

		var storedPasswordHash string
		var userID int
		var userName string

		ctxDB, cancelDB := context.WithTimeout(r.Context(), dbTimeout)
		defer cancelDB()

		err := db.QueryRowContext(ctxDB, "SELECT id, name, password_hash FROM users WHERE email = ?", email).Scan(&userID, &userName, &storedPasswordHash)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				data.Message = "Invalid email or password."
			} else if errors.Is(err, context.DeadlineExceeded) {
				log.Printf("loginHandler: DB query timeout for email %s: %v", email, err)
				data.Message = "Login service temporarily unavailable. Please try again later."
				renderTemplateWithStatus(w, r, http.StatusServiceUnavailable, loginTemplate, "base.gohtml", data)
				return
			} else {
				log.Printf("loginHandler: Database error for email %s: %v", email, err)
				data.Message = "An error occurred. Please try again."
			}
			renderTemplateWithStatus(w, r, http.StatusUnauthorized, loginTemplate, "base.gohtml", data)
			return
		}

		ctxHash, cancelHash := context.WithTimeout(r.Context(), hashTimeout)
		defer cancelHash()
		matchCh := make(chan error, 1)
		go func() {
			matchCh <- bcrypt.CompareHashAndPassword([]byte(storedPasswordHash), []byte(password))
		}()

		select {
		case <-ctxHash.Done():
			log.Printf("loginHandler: Password comparison timeout for user ID %d", userID)
			data.Message = "Login service temporarily unavailable (timeout). Please try again later."
			renderTemplateWithStatus(w, r, http.StatusServiceUnavailable, loginTemplate, "base.gohtml", data)
			return
		case err := <-matchCh:
			if err != nil {
				data.Message = "Invalid email or password."
				renderTemplateWithStatus(w, r, http.StatusUnauthorized, loginTemplate, "base.gohtml", data)
				return
			}
		}

		if err := sessionManager.RenewToken(r.Context()); err != nil {
			log.Printf("loginHandler: Error renewing session token for user ID %d: %v", userID, err)
			data.Message = "An error occurred during login. Please try again."
			renderTemplateWithStatus(w, r, http.StatusInternalServerError, loginTemplate, "base.gohtml", data)
			return
		}

		sessionManager.Put(r.Context(), sessionUserIDKey, userID)
		sessionManager.Put(r.Context(), sessionUserNameKey, userName)
		sessionManager.Put(r.Context(), sessionAuthTimeKey, time.Now())

		log.Printf("User %s (ID: %d) logged in successfully.", userName, userID)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

func signupHandler(w http.ResponseWriter, r *http.Request) {
	if sessionManager == nil {
		log.Println("signupHandler: CRITICAL: sessionManager is nil.")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if sessionManager.Exists(r.Context(), sessionUserIDKey) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	data := PageData{Title: "Signup - Scramble Run"}

	if r.Method == http.MethodGet {
		renderTemplateWithStatus(w, r, http.StatusOK, signupTemplate, "base.gohtml", data)
		return
	}

	if r.Method == http.MethodPost {
		// TODO: Validate CSRF token here.

		r.Body = http.MaxBytesReader(w, r.Body, maxFormMemory)
		if err := r.ParseForm(); err != nil {
			if _, ok := err.(*http.MaxBytesError); ok {
				log.Printf("signupHandler: Form too large: %v", err)
				data.Message = "The submitted form is too large. Please try again."
				renderTemplateWithStatus(w, r, http.StatusRequestEntityTooLarge, signupTemplate, "base.gohtml", data)
				return
			}
			log.Printf("signupHandler: Error parsing form: %v", err)
			data.Message = "Error processing form. Please try again."
			renderTemplateWithStatus(w, r, http.StatusBadRequest, signupTemplate, "base.gohtml", data)
			return
		}

		name := strings.TrimSpace(r.FormValue("name"))
		email := normalizeEmail(r.FormValue("email"))
		password := r.FormValue("password")
		confirmPassword := r.FormValue("confirm_password")

		if name == "" || email == "" || password == "" {
			data.Message = "All fields (Name, Email, Password) are required."
			renderTemplateWithStatus(w, r, http.StatusBadRequest, signupTemplate, "base.gohtml", data)
			return
		}
		if len(name) > 100 {
			data.Message = "Name is too long (max 100 characters)."
			renderTemplateWithStatus(w, r, http.StatusBadRequest, signupTemplate, "base.gohtml", data)
			return
		}
		if !isValidEmail(email) || len(email) > 254 {
			data.Message = "Invalid email format or email too long."
			renderTemplateWithStatus(w, r, http.StatusBadRequest, signupTemplate, "base.gohtml", data)
			return
		}
		if len(password) < minPasswordLength {
			data.Message = fmt.Sprintf("Password must be at least %d characters.", minPasswordLength)
			renderTemplateWithStatus(w, r, http.StatusBadRequest, signupTemplate, "base.gohtml", data)
			return
		}
		if len(password) > 256 {
			data.Message = "Password is too long."
			renderTemplateWithStatus(w, r, http.StatusBadRequest, signupTemplate, "base.gohtml", data)
			return
		}
		if password != confirmPassword {
			data.Message = "Passwords do not match."
			renderTemplateWithStatus(w, r, http.StatusBadRequest, signupTemplate, "base.gohtml", data)
			return
		}

		ctxDBCheck, cancelDBCheck := context.WithTimeout(r.Context(), dbTimeout)
		defer cancelDBCheck()
		var count int
		err := db.QueryRowContext(ctxDBCheck, "SELECT COUNT(*) FROM users WHERE email = ?", email).Scan(&count)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				log.Printf("signupHandler: DB query timeout checking email %s: %v", email, err)
				data.Message = "Signup service temporarily unavailable. Please try again later."
			} else {
				log.Printf("signupHandler: Database error checking email %s: %v", email, err)
				data.Message = "An error occurred. Please try again."
			}
			renderTemplateWithStatus(w, r, http.StatusInternalServerError, signupTemplate, "base.gohtml", data)
			return
		}
		if count > 0 {
			data.Message = "Email address is already in use."
			renderTemplateWithStatus(w, r, http.StatusConflict, signupTemplate, "base.gohtml", data)
			return
		}

		ctxHash, cancelHash := context.WithTimeout(r.Context(), hashTimeout)
		defer cancelHash()
		var hashedPassword []byte
		hashCh := make(chan struct {
			hash []byte
			err  error
		}, 1)
		go func() {
			h, e := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
			hashCh <- struct {
				hash []byte
				err  error
			}{h, e}
		}()

		select {
		case <-ctxHash.Done():
			log.Printf("signupHandler: Password hashing timeout for email %s", email)
			data.Message = "Signup service temporarily unavailable (timeout). Please try again later."
			renderTemplateWithStatus(w, r, http.StatusServiceUnavailable, signupTemplate, "base.gohtml", data)
			return
		case res := <-hashCh:
			if res.err != nil {
				log.Printf("signupHandler: Error hashing password for email %s: %v", email, res.err)
				data.Message = "An error occurred during registration. Please try again."
				renderTemplateWithStatus(w, r, http.StatusInternalServerError, signupTemplate, "base.gohtml", data)
				return
			}
			hashedPassword = res.hash
		}

		ctxDBInsert, cancelDBInsert := context.WithTimeout(r.Context(), dbTimeout)
		defer cancelDBInsert()
		_, err = db.ExecContext(ctxDBInsert, "INSERT INTO users (name, email, password_hash, created_at, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
			name, email, string(hashedPassword))
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				log.Printf("signupHandler: DB insert timeout for email %s: %v", email, err)
				data.Message = "Signup service temporarily unavailable. Please try again later."
			} else {
				log.Printf("signupHandler: Error inserting user %s: %v", email, err)
				data.Message = "An error occurred during registration. Please try again."
			}
			renderTemplateWithStatus(w, r, http.StatusInternalServerError, signupTemplate, "base.gohtml", data)
			return
		}

		log.Printf("New user registered: Name: %s, Email: %s", name, email)
		sessionManager.Put(r.Context(), "flash_message", "Registration successful! You can now log in.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	if sessionManager == nil {
		log.Println("logoutHandler: CRITICAL: sessionManager is nil.")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	// TODO: Validate CSRF token for logout POST requests.

	userID := sessionManager.GetInt(r.Context(), sessionUserIDKey)

	if err := sessionManager.Destroy(r.Context()); err != nil {
		log.Printf("logoutHandler: Error destroying session for user ID %d: %v", userID, err)
	}

	log.Printf("User ID %d logged out.", userID)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func requireAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sessionManager == nil {
			log.Println("requireAuthentication: CRITICAL: sessionManager is nil.")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError) // Or redirect to an error page
			return
		}
		if !sessionManager.Exists(r.Context(), sessionUserIDKey) {
			sessionManager.Put(r.Context(), "redirect_after_login", r.URL.Path)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		w.Header().Add("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

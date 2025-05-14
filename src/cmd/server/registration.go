package main

import (
	"database/sql"
	"golang.org/x/crypto/bcrypt"
	"log"
	"net/http"
	"strings"
)

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

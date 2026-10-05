package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID           int    `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"passwordHash"`
}

type Task struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	DueDate     string `json:"dueDate"`
	UserID      int    `json:"userId"`
}

type DB struct {
	Users      []User `json:"users"`
	Tasks      []Task `json:"tasks"`
	NextUserID int    `json:"nextUserId"`
	NextTaskID int    `json:"nextTaskId"`
}

type App struct {
	mu        sync.Mutex
	db        DB
	dataFile  string
	jwtSecret []byte
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type taskInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	DueDate     string `json:"dueDate"`
}

type taskPatch struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
	Priority    *string `json:"priority"`
	DueDate     *string `json:"dueDate"`
}

type contextKey string

const userIDKey contextKey = "userID"

func main() {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "dev-secret-change-me"
	}

	app := &App{
		dataFile:  filepath.Join(".", "data.json"),
		jwtSecret: []byte(secret),
		db: DB{
			Users:      []User{},
			Tasks:      []Task{},
			NextUserID: 1,
			NextTaskID: 1,
		},
	}
	if err := app.load(); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/register", app.register)
	mux.HandleFunc("POST /api/auth/login", app.login)
	mux.Handle("GET /api/tasks", app.auth(http.HandlerFunc(app.listTasks)))
	mux.Handle("GET /api/tasks/{id}", app.auth(http.HandlerFunc(app.getTask)))
	mux.Handle("POST /api/tasks", app.auth(http.HandlerFunc(app.createTask)))
	mux.Handle("PATCH /api/tasks/{id}", app.auth(http.HandlerFunc(app.updateTask)))
	mux.Handle("DELETE /api/tasks/{id}", app.auth(http.HandlerFunc(app.deleteTask)))

	handler := cors(mux)
	log.Println("backend started: http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}

func (a *App) load() error {
	data, err := os.ReadFile(a.dataFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, &a.db)
}

func (a *App) saveLocked() error {
	data, err := json.MarshalIndent(a.db, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.dataFile, data, 0644)
}

func (a *App) register(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Email == "" || len(in.Password) < 6 {
		writeError(w, http.StatusBadRequest, "email is required; password must be at least 6 characters")
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	for _, u := range a.db.Users {
		if u.Email == in.Email {
			writeError(w, http.StatusConflict, "user already exists")
			return
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	user := User{ID: a.db.NextUserID, Email: in.Email, PasswordHash: string(hash)}
	a.db.NextUserID++
	a.db.Users = append(a.db.Users, user)
	if err := a.saveLocked(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save user")
		return
	}

	token, err := a.makeToken(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "user": map[string]any{"id": user.ID, "email": user.Email}})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))

	a.mu.Lock()
	defer a.mu.Unlock()

	for _, u := range a.db.Users {
		if u.Email == in.Email && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) == nil {
			token, err := a.makeToken(u.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to create token")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": map[string]any{"id": u.ID, "email": u.Email}})
			return
		}
	}
	writeError(w, http.StatusUnauthorized, "invalid email or password")
}

func (a *App) listTasks(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(userIDKey).(int)
	status := r.URL.Query().Get("status")
	if status != "" && !validStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	result := make([]Task, 0)
	for _, t := range a.db.Tasks {
		if t.UserID == userID && (status == "" || t.Status == status) {
			result = append(result, t)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) getTask(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(userIDKey).(int)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	for _, t := range a.db.Tasks {
		if t.ID == id && t.UserID == userID {
			writeJSON(w, http.StatusOK, t)
			return
		}
	}
	writeError(w, http.StatusNotFound, "task not found")
}

func (a *App) createTask(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(userIDKey).(int)
	var in taskInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := validateTaskInput(in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	task := Task{
		ID:          a.db.NextTaskID,
		Title:       strings.TrimSpace(in.Title),
		Description: strings.TrimSpace(in.Description),
		Status:      in.Status,
		Priority:    in.Priority,
		DueDate:     in.DueDate,
		UserID:      userID,
	}
	a.db.NextTaskID++
	a.db.Tasks = append(a.db.Tasks, task)
	if err := a.saveLocked(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save task")
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (a *App) updateTask(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(userIDKey).(int)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	var in taskPatch
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if in.Status != nil && !validStatus(*in.Status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if in.Priority != nil && !validPriority(*in.Priority) {
		writeError(w, http.StatusBadRequest, "invalid priority")
		return
	}
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		writeError(w, http.StatusBadRequest, "title cannot be empty")
		return
	}
	if in.DueDate != nil && *in.DueDate != "" {
		if _, err := time.Parse("2006-01-02", *in.DueDate); err != nil {
			writeError(w, http.StatusBadRequest, "dueDate must be YYYY-MM-DD")
			return
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	for i := range a.db.Tasks {
		t := &a.db.Tasks[i]
		if t.ID == id && t.UserID == userID {
			if in.Title != nil {
				t.Title = strings.TrimSpace(*in.Title)
			}
			if in.Description != nil {
				t.Description = strings.TrimSpace(*in.Description)
			}
			if in.Status != nil {
				t.Status = *in.Status
			}
			if in.Priority != nil {
				t.Priority = *in.Priority
			}
			if in.DueDate != nil {
				t.DueDate = *in.DueDate
			}

			if err := a.saveLocked(); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to save task")
				return
			}
			writeJSON(w, http.StatusOK, *t)
			return
		}
	}
	writeError(w, http.StatusNotFound, "task not found")
}

func (a *App) deleteTask(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(userIDKey).(int)
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	for i, t := range a.db.Tasks {
		if t.ID == id && t.UserID == userID {
			a.db.Tasks = append(a.db.Tasks[:i], a.db.Tasks[i+1:]...)
			if err := a.saveLocked(); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to save")
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	writeError(w, http.StatusNotFound, "task not found")
}

func validateTaskInput(in taskInput) error {
	if strings.TrimSpace(in.Title) == "" {
		return errors.New("title is required")
	}
	if !validStatus(in.Status) {
		return errors.New("status must be todo, in_progress or done")
	}
	if !validPriority(in.Priority) {
		return errors.New("priority must be low, medium or high")
	}
	if in.DueDate != "" {
		if _, err := time.Parse("2006-01-02", in.DueDate); err != nil {
			return errors.New("dueDate must be YYYY-MM-DD")
		}
	}
	return nil
}

func validStatus(v string) bool {
	return v == "todo" || v == "in_progress" || v == "done"
}

func validPriority(v string) bool {
	return v == "low" || v == "medium" || v == "high"
}

func (a *App) makeToken(userID int) (string, error) {
	claims := jwt.MapClaims{
		"sub": fmt.Sprint(userID),
		"exp": time.Now().Add(24 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.jwtSecret)
}

func (a *App) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing token")
			return
		}
		raw := strings.TrimPrefix(header, "Bearer ")
		token, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
			if t.Method != jwt.SigningMethodHS256 {
				return nil, errors.New("unexpected signing method")
			}
			return a.jwtSecret, nil
		})
		if err != nil || !token.Valid {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid token claims")
			return
		}
		sub, err := claims.GetSubject()
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token subject")
			return
		}
		userID, err := strconv.Atoi(sub)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token subject")
			return
		}

		ctx := r.Context()
		ctx = contextWithUserID(ctx, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func contextWithUserID(ctx context.Context, userID int) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

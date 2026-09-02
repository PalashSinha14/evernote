package schemas

import "time"

// SignupRequest is the body of POST /api/v1/auth/signup.
//
// The binding tags are enforced by Gin before the handler runs, so an invalid
// body never reaches business logic. The password maximum is bcrypt's 72-byte
// input limit: beyond it the algorithm truncates silently, which would make
// two different passwords interchangeable.
type SignupRequest struct {
	Name     string `json:"name"     binding:"required,min=1,max=100"`
	Email    string `json:"email"    binding:"required,email,max=254"`
	Password string `json:"password" binding:"required,min=8,max=72"`
}

// SignupResponse is the 201 body of POST /api/v1/auth/signup.
type SignupResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// LoginRequest is the body of POST /api/v1/auth/login.
type LoginRequest struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse is the 200 body of POST /api/v1/auth/login. ExpiresIn is the
// token's lifetime in seconds.
type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}

// MeResponse is the 200 body of GET /api/v1/me.
type MeResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Email     string     `json:"email"`
	CreatedAt time.Time  `json:"created_at"`
	LastLogin *time.Time `json:"last_login,omitempty"`
}

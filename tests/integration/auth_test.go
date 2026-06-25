//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper response types for JSON parsing.
type authLoginData struct {
	Data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	} `json:"data"`
}

type authErrorResp struct {
	Error string `json:"error"`
}

type authMeData struct {
	Data struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Name  string `json:"name"`
			Role  string `json:"role"`
		} `json:"user"`
	} `json:"data"`
}

type authRegisterData struct {
	Data struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"user"`
	} `json:"data"`
}

func Test_CustomerRegistersAndLogsIn(t *testing.T) {
	env := getTestEnv()

	email := "cust_register@test.com"

	// Register
	resp, body, err := doJSON(http.MethodPost, env.authURL+"/api/auth/register", map[string]interface{}{
		"email":    email,
		"password": "Test123!",
		"name":     "Cust Test",
		"role":     "customer",
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)

	// Login
	resp, body, err = doJSON(http.MethodPost, env.authURL+"/api/auth/login", map[string]string{
		"email":    email,
		"password": "Test123!",
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var lr authLoginData
	mustJSON(body, &lr)
	assert.NotEmpty(t, lr.Data.AccessToken)
	assert.NotEmpty(t, lr.Data.RefreshToken)

	// Verify token gives us /me
	resp, body, err = doJSON(http.MethodGet, env.authURL+"/api/auth/me", nil, map[string]string{
		"Authorization": "Bearer " + lr.Data.AccessToken,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var me authMeData
	mustJSON(body, &me)
	assert.Equal(t, email, me.Data.User.Email)
	assert.Equal(t, "customer", me.Data.User.Role)
}

func Test_EORegistersAndLogsIn(t *testing.T) {
	env := getTestEnv()

	email := "eo_register@test.com"

	// Register via organizer endpoint
	resp, body, err := doJSON(http.MethodPost, env.authURL+"/api/auth/register/organizer", map[string]interface{}{
		"email":          email,
		"password":       "Test123!",
		"name":           "EO User",
		"organizer_name": "Big Org",
		"description":    "We do events",
		"profile_link":   "https://bigorg.test",
		"contact_email":  email,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, 201, resp.StatusCode)
	var rd authRegisterData
	mustJSON(body, &rd)
	assert.Equal(t, "eo", rd.Data.User.Role)

	// Login
	resp, body, err = doJSON(http.MethodPost, env.authURL+"/api/auth/login", map[string]string{
		"email":    email,
		"password": "Test123!",
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var lr authLoginData
	mustJSON(body, &lr)
	assert.NotEmpty(t, lr.Data.AccessToken)
	assert.NotEmpty(t, lr.Data.RefreshToken)
}

func Test_DuplicateEmailRegistrationReturnsConflict(t *testing.T) {
	env := getTestEnv()

	email := "dupe@test.com"

	// First registration
	doJSON(http.MethodPost, env.authURL+"/api/auth/register", map[string]interface{}{
		"email": email, "password": "Test123!", "name": "First", "role": "customer",
	}, nil)

	// Second registration should fail
	resp, _, err := doJSON(http.MethodPost, env.authURL+"/api/auth/register", map[string]interface{}{
		"email": email, "password": "Test123!", "name": "Second", "role": "customer",
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, 409, resp.StatusCode)
}

func Test_UnauthorizedAccessReturns401(t *testing.T) {
	env := getTestEnv()

	// Auth routes: missing Authorization
	resp, _, err := doJSON(http.MethodGet, env.authURL+"/api/auth/me", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode)

	// Event routes: missing X-auth headers (RequireRole middleware)
	resp, _, err = doJSON(http.MethodPost, env.eventURL+"/api/events", map[string]string{
		"title": "Bad",
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}

func Test_AdminLogin(t *testing.T) {
	env := getTestEnv()

	resp, body, err := doJSON(http.MethodPost, env.authURL+"/api/auth/login", map[string]string{
		"email":    "admin@test.com",
		"password": "Admin123!",
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var lr authLoginData
	mustJSON(body, &lr)
	assert.NotEmpty(t, lr.Data.AccessToken)

	// Verify role via /me
	resp, body, err = doJSON(http.MethodGet, env.authURL+"/api/auth/me", nil, map[string]string{
		"Authorization": "Bearer " + lr.Data.AccessToken,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var me authMeData
	mustJSON(body, &me)
	assert.Equal(t, "admin", me.Data.User.Role)
}

func Test_TokenRefreshAndOldTokenRotation(t *testing.T) {
	env := getTestEnv()

	email := "refresh_test@test.com"
	pass := "Test123!"

	// Register
	doJSON(http.MethodPost, env.authURL+"/api/auth/register", map[string]interface{}{
		"email": email, "password": pass, "name": "Refresh Test", "role": "customer",
	}, nil)

	// Login to get tokens
	_, body, _ := doJSON(http.MethodPost, env.authURL+"/api/auth/login", map[string]string{
		"email": email, "password": pass,
	}, nil)
	var lr authLoginData
	mustJSON(body, &lr)

	oldRefresh := lr.Data.RefreshToken
	oldAccess := lr.Data.AccessToken

	// Refresh
	resp, body, err := doJSON(http.MethodPost, env.authURL+"/api/auth/refresh", map[string]string{
		"refresh_token": oldRefresh,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	var newLR authLoginData
	mustJSON(body, &newLR)
	assert.NotEmpty(t, newLR.Data.AccessToken)
	assert.NotEmpty(t, newLR.Data.RefreshToken)

	// Old refresh token should now be rejected (rotation)
	resp, _, err = doJSON(http.MethodPost, env.authURL+"/api/auth/refresh", map[string]string{
		"refresh_token": oldRefresh,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode)

	// Old access token should still work (still valid)
	resp, _, err = doJSON(http.MethodGet, env.authURL+"/api/auth/me", nil, map[string]string{
		"Authorization": "Bearer " + oldAccess,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

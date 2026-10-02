//go:build integration

package integration

// auth_test.go — TestAuth_*: registration, login, roles, token refresh.

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuth_RegisterCustomer_AndLogin(t *testing.T) {
	env := getTestEnv()

	email := "cust_register@test.com"

	resp, body, err := doJSON(http.MethodPost, env.authURL+"/api/auth/register", map[string]interface{}{
		"email":    email,
		"password": "Test123!",
		"name":     "Cust Test",
		"role":     "customer",
	}, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "%s", body)

	resp, body, err = doJSON(http.MethodPost, env.authURL+"/api/auth/login", map[string]string{
		"email":    email,
		"password": "Test123!",
	}, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)

	var lr authLoginData
	mustJSON(body, &lr)
	assert.NotEmpty(t, lr.Data.AccessToken)
	assert.NotEmpty(t, lr.Data.RefreshToken)

	// The issued access token identifies the user.
	resp, body, err = doJSON(http.MethodGet, env.authURL+"/api/auth/me", nil, bearer(lr.Data.AccessToken))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)

	var me authMeData
	mustJSON(body, &me)
	assert.Equal(t, email, me.Data.User.Email)
	assert.Equal(t, "customer", me.Data.User.Role)
}

func TestAuth_RegisterEO_AndLogin(t *testing.T) {
	env := getTestEnv()

	email := "eo_register@test.com"

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
	require.Equal(t, http.StatusCreated, resp.StatusCode, "%s", body)

	var rd authRegisterData
	mustJSON(body, &rd)
	assert.Equal(t, "eo", rd.Data.User.Role)

	resp, body, err = doJSON(http.MethodPost, env.authURL+"/api/auth/login", map[string]string{
		"email":    email,
		"password": "Test123!",
	}, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)

	var lr authLoginData
	mustJSON(body, &lr)
	assert.NotEmpty(t, lr.Data.AccessToken)
	assert.NotEmpty(t, lr.Data.RefreshToken)
}

func TestAuth_Register_DuplicateEmail_Conflict(t *testing.T) {
	env := getTestEnv()

	email := "dupe@test.com"

	resp, body, err := doJSON(http.MethodPost, env.authURL+"/api/auth/register", map[string]interface{}{
		"email": email, "password": "Test123!", "name": "First", "role": "customer",
	}, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "%s", body)

	resp, body, err = doJSON(http.MethodPost, env.authURL+"/api/auth/register", map[string]interface{}{
		"email": email, "password": "Test123!", "name": "Second", "role": "customer",
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, resp.StatusCode, "%s", body)
}

func TestAuth_UnauthorizedAccess_401(t *testing.T) {
	env := getTestEnv()

	// /api/auth/me requires a bearer token.
	resp, body, err := doJSON(http.MethodGet, env.authURL+"/api/auth/me", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "%s", body)
}

func TestAuth_LoginAdmin_Success(t *testing.T) {
	env := getTestEnv()

	resp, body, err := doJSON(http.MethodPost, env.authURL+"/api/auth/login", map[string]string{
		"email":    adminEmail,
		"password": adminPassword,
	}, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)

	var lr authLoginData
	mustJSON(body, &lr)
	assert.NotEmpty(t, lr.Data.AccessToken)

	resp, body, err = doJSON(http.MethodGet, env.authURL+"/api/auth/me", nil, bearer(lr.Data.AccessToken))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)

	var me authMeData
	mustJSON(body, &me)
	assert.Equal(t, "admin", me.Data.User.Role)
}

// TestAuth_RefreshToken_Rotation verifies a refresh issues a new pair, retires
// the old refresh token, and leaves the old access token usable until it expires.
func TestAuth_RefreshToken_Rotation(t *testing.T) {
	env := getTestEnv()

	c := env.registerAndLogin("customer")
	oldRefresh := c.RefreshToken
	oldAccess := c.AccessToken

	resp, body, err := doJSON(http.MethodPost, env.authURL+"/api/auth/refresh", map[string]string{
		"refresh_token": oldRefresh,
	}, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)

	var newLR authLoginData
	mustJSON(body, &newLR)
	assert.NotEmpty(t, newLR.Data.AccessToken)
	assert.NotEmpty(t, newLR.Data.RefreshToken)

	// The old refresh token is retired.
	resp, body, err = doJSON(http.MethodPost, env.authURL+"/api/auth/refresh", map[string]string{
		"refresh_token": oldRefresh,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "%s", body)

	// The old access token is still valid.
	resp, body, err = doJSON(http.MethodGet, env.authURL+"/api/auth/me", nil, bearer(oldAccess))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)
}

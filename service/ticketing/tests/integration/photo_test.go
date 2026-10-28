//go:build integration

package integration

// photo_test.go — TestPhoto_*: event photo upload through the public API, with
// pkg/s3 pointed at the in-process fake S3 endpoint (signing included).

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pngBytes starts with a real PNG signature: the handler sniffs content types
// from the bytes, so a renamed text file must not pass as an image.
var pngBytes = append(
	[]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
	bytes.Repeat([]byte{0}, 16)...,
)

// gifBytes has the GIF signature — a real image format we deliberately reject.
var gifBytes = append([]byte("GIF89a"), bytes.Repeat([]byte{0}, 16)...)

func postMultipart(t *testing.T, url, field, filename string, data []byte, headers map[string]string) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(field, filename)
	require.NoError(t, err)
	_, err = fw.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	req, err := http.NewRequest(http.MethodPost, url, &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", w.FormDataContentType())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}

func TestPhoto_UploadAndFetch(t *testing.T) {
	env := getTestEnv()
	eo := newEO()
	ev := createEventRaw(t, env, eo)

	code, body := postMultipart(
		t, env.apiURL+"/api/events/"+ev.ID+"/photo", "photo", "poster.png", pngBytes, eo.headers,
	)
	require.Equal(t, http.StatusOK, code, "%s", body)

	var resp struct {
		Data struct {
			ImageURL string `json:"image_url"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	prefix := env.fakeS3.url + "/test-bucket/events/" + ev.ID + "/"
	assert.True(t, strings.HasPrefix(resp.Data.ImageURL, prefix),
		"image_url %q must point into the fake bucket under the event", resp.Data.ImageURL)

	// The object really landed in the fake store under the key in the URL
	// (the fake stores paths verbatim, bucket segment included).
	key := strings.TrimPrefix(resp.Data.ImageURL, env.fakeS3.url+"/")
	assert.NotEmpty(t, env.fakeS3.get(key), "uploaded object missing for key %q", key)

	// The public event detail carries the same URL.
	resp2, body2, err := doJSON(http.MethodGet, env.apiURL+"/api/events/"+ev.ID, nil, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp2.StatusCode, "%s", body2)
	var detail struct {
		Data eventDetailResp `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body2, &detail))
	assert.Equal(t, resp.Data.ImageURL, detail.Data.Event.ImageURL)
}

func TestPhoto_RejectsForeignOwner(t *testing.T) {
	env := getTestEnv()
	ev := createEventRaw(t, env, newEO())

	stranger := newEO()
	code, body := postMultipart(
		t, env.apiURL+"/api/events/"+ev.ID+"/photo", "photo", "poster.png", pngBytes, stranger.headers,
	)
	assert.Equal(t, http.StatusNotFound, code, "another EO must not see the event: %s", body)
}

func TestPhoto_RejectsUnsupportedType(t *testing.T) {
	env := getTestEnv()
	eo := newEO()
	ev := createEventRaw(t, env, eo)

	code, body := postMultipart(
		t, env.apiURL+"/api/events/"+ev.ID+"/photo", "photo", "animation.gif", gifBytes, eo.headers,
	)
	assert.Equal(t, http.StatusBadRequest, code, "gif must be rejected: %s", body)
}

func TestPhoto_RejectsOversized(t *testing.T) {
	env := getTestEnv()
	eo := newEO()
	ev := createEventRaw(t, env, eo)

	// Above the 5 MB service limit but below the 6 MB body cap, so the
	// service-level check is what rejects this one.
	big := append(append([]byte{}, pngBytes...), bytes.Repeat([]byte{0}, 5<<20)...)
	code, body := postMultipart(
		t, env.apiURL+"/api/events/"+ev.ID+"/photo", "photo", "huge.png", big, eo.headers,
	)
	assert.Equal(t, http.StatusBadRequest, code, "oversized image must be rejected: %s", body)
}

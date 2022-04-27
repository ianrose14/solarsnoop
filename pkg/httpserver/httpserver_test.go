package httpserver_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ianrose14/solarsnoop/pkg/httpserver"
	"github.com/stretchr/testify/assert"
)

func TestResponseWriterPeeker(t *testing.T) {
	testCases := []struct {
		name         string
		handler      http.HandlerFunc
		expectedCode int
		expectedBody string
	}{
		{
			name: "test ok",
			handler: func(rw http.ResponseWriter, r *http.Request) {
				w := httpserver.NewResponseWriterPeeker(rw)

				assert.Equal(t, 0, w.GetStatus(), "wrong internal status")
				assert.Equal(t, 0, w.GetContentLength(), "wrong internal content length")

				fmt.Fprintln(w, "chicken")

				assert.Equal(t, http.StatusOK, w.GetStatus(), "wrong internal status")
				assert.Equal(t, 8, w.GetContentLength(), "wrong internal content length")
			},
			expectedCode: http.StatusOK,
			expectedBody: "chicken\n",
		},
		{
			name: "test ok two writes",
			handler: func(rw http.ResponseWriter, r *http.Request) {
				w := httpserver.NewResponseWriterPeeker(rw)

				assert.Equal(t, 0, w.GetStatus(), "wrong internal status")
				assert.Equal(t, 0, w.GetContentLength(), "wrong internal content length")

				fmt.Fprintf(w, "chicken")
				fmt.Fprintf(w, " soup")

				assert.Equal(t, http.StatusOK, w.GetStatus(), "wrong internal status")
				assert.Equal(t, 12, w.GetContentLength(), "wrong internal content length")
			},
			expectedCode: http.StatusOK,
			expectedBody: "chicken soup",
		},
		{
			name: "test not found",
			handler: func(rw http.ResponseWriter, r *http.Request) {
				w := httpserver.NewResponseWriterPeeker(rw)
				http.NotFound(w, r)

				assert.Equal(t, http.StatusNotFound, w.GetStatus(), "wrong internal status")
				assert.Equal(t, 19, w.GetContentLength(), "wrong internal content length")
			},
			expectedCode: http.StatusNotFound,
			expectedBody: "404 page not found\n",
		},
		{
			name: "test internal server error",
			handler: func(rw http.ResponseWriter, r *http.Request) {
				w := httpserver.NewResponseWriterPeeker(rw)
				http.Error(w, "sorry, it blew up", http.StatusInternalServerError)

				assert.Equal(t, http.StatusInternalServerError, w.GetStatus(), "wrong internal status")
				assert.Equal(t, 18, w.GetContentLength(), "wrong internal content length")
			},
			expectedCode: http.StatusInternalServerError,
			expectedBody: "sorry, it blew up\n",
		},
		{
			name: "test double status explicit",
			handler: func(rw http.ResponseWriter, r *http.Request) {
				w := httpserver.NewResponseWriterPeeker(rw)

				w.WriteHeader(http.StatusAccepted)
				fmt.Fprintf(w, "hi bob")
				w.WriteHeader(http.StatusFound)

				assert.Equal(t, http.StatusAccepted, w.GetStatus(), "wrong internal status")
				assert.Equal(t, 6, w.GetContentLength(), "wrong internal content length")
			},
			expectedCode: http.StatusAccepted,
			expectedBody: "hi bob",
		},
		{
			name: "test double status implicit",
			handler: func(rw http.ResponseWriter, r *http.Request) {
				w := httpserver.NewResponseWriterPeeker(rw)

				fmt.Fprintf(w, "superman")
				w.WriteHeader(http.StatusNotFound)

				assert.Equal(t, http.StatusOK, w.GetStatus(), "wrong internal status")
				assert.Equal(t, 8, w.GetContentLength(), "wrong internal content length")
			},
			expectedCode: http.StatusOK,
			expectedBody: "superman",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			svr := httptest.NewServer(testCase.handler)
			defer svr.Close()

			rsp, err := http.Get(svr.URL)
			assert.NoError(t, err, "failed to GET")
			assert.Equal(t, testCase.expectedCode, rsp.StatusCode, "wrong external status code")

			body, err := io.ReadAll(rsp.Body)
			rsp.Body.Close()
			assert.NoError(t, err, "failed to read from response")
			assert.Equal(t, testCase.expectedBody, string(body), "bad response body")
		})
	}
}

package publicartifacts

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegressionDownloadPercentFilename(t *testing.T) {
	for _, name := range []string{"report.txt", "100%.txt", "literal%20.txt"} {
		t.Run(name, func(t *testing.T) {
			s := New(t.TempDir(), "https://example.test", 8765)
			result, err := s.PublishBytes(PublishBytesRequest{Filename: name, Data: []byte("ok")})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, result.URL, nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r, "/artifacts/public/")
			if w.Code != http.StatusOK {
				t.Fatalf("published URL returned %d; decoded path=%q", w.Code, r.URL.Path)
			}
		})
	}
}

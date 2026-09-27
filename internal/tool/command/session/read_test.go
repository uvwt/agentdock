package session

import (
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestReadRetriesAndIndependentObservers(t *testing.T) {
	s := &Session{ID: "replay", StartedAt: time.Now()}
	_, _ = (sessionOutputWriter{session: s}).Write([]byte("first-second"))
	_, _ = (sessionOutputWriter{session: s, stderr: true}).Write([]byte("error"))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 5 {
				page, err := s.Read(0, 0, 5)
				if err != nil || page.Stdout != "first" || page.Stderr != "error" || page.StdoutNextOffset != 5 {
					t.Errorf("retry = %#v, %v", page, err)
				}
			}
		})
	}
	wg.Wait()
	page, err := s.Read(5, 5, 20)
	if err != nil || page.Stdout != "-second" || page.Stderr != "" {
		t.Fatalf("next page = %#v, %v", page, err)
	}
	if legacy := s.Snapshot("running", 100); legacy.Stdout != "first-second" {
		t.Fatalf("read consumed legacy output: %#v", legacy)
	}
	page, err = s.Read(0, 0, 100)
	if err != nil || page.Stdout != "first-second" {
		t.Fatalf("legacy status affected replay: %#v, %v", page, err)
	}
}

func TestReadPaginatesUTF8AndWaitsForIncompleteCharacter(t *testing.T) {
	s := &Session{StartedAt: time.Now()}
	text := "A\u4f60\U0001f680Z"
	writer := sessionOutputWriter{session: s}
	_, _ = writer.Write([]byte(text[:6]))
	page, err := s.Read(0, 0, 100)
	if err != nil || page.Stdout != text[:4] || page.StdoutNextOffset != 4 {
		t.Fatalf("partial character = %#v, %v", page, err)
	}
	_, _ = writer.Write([]byte(text[6:]))
	var output strings.Builder
	for offset := 0; offset < len(text); {
		page, err := s.Read(offset, 0, 4)
		if err != nil || !utf8.ValidString(page.Stdout) || page.StdoutNextOffset <= offset {
			t.Fatalf("UTF-8 page = %#v, %v", page, err)
		}
		output.WriteString(page.Stdout)
		offset = page.StdoutNextOffset
	}
	if output.String() != text {
		t.Fatalf("output = %q, want %q", output.String(), text)
	}
}

func TestReadReportsEvictedOutputAndValidatesOffsets(t *testing.T) {
	s := &Session{StartedAt: time.Now()}
	text := strings.Repeat("x", 4*1024*1024) + "tail"
	_, _ = (sessionOutputWriter{session: s}).Write([]byte(text))
	page, err := s.Read(0, 0, 4)
	if err != nil || page.StdoutOffset != 4 || page.StdoutMissedBytes != 4 || page.StdoutNextOffset != 8 {
		t.Fatalf("evicted output = %#v, %v", page, err)
	}
	for _, offsets := range [][2]int{{-1, 0}, {len(text) + 1, 0}, {0, -1}, {0, 1}} {
		if _, err := s.Read(offsets[0], offsets[1], 4); err == nil {
			t.Fatalf("accepted invalid offsets %v", offsets)
		}
	}
	if _, err := s.Read(0, 0, 3); err == nil {
		t.Fatal("accepted a page smaller than UTF-8's maximum character size")
	}
}

func TestReadReportsUTF8GapAndCompletion(t *testing.T) {
	s := &Session{StartedAt: time.Now(), completed: true, exitCode: 7}
	_, _ = (sessionOutputWriter{session: s}).Write([]byte("\u4f60ok"))
	page, err := s.Read(1, 0, 10)
	if err != nil || page.Stdout != "ok" || page.StdoutOffset != 3 || page.StdoutMissedBytes != 2 || page.Status != "exited" || page.CommandOK {
		t.Fatalf("completed page = %#v, %v", page, err)
	}
}

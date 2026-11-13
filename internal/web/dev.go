package web

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"time"
)

// devPoll is how often devEvents checks templates and static files for changes.
const devPoll = 250 * time.Millisecond

type devEvent struct {
	Boot    string `json:"boot,omitempty"`
	Changed string `json:"changed,omitempty"`
}

// devEvents streams server-sent events to dev-reload.js: this process's boot id
// on connect, then the path of each template or static file that changes.
func (s *server) devEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	rc := http.NewResponseController(w)
	send := func(e devEvent) error {
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return err
		}
		return rc.Flush()
	}

	if send(devEvent{Boot: s.boot}) != nil {
		return
	}
	prev := s.devSnapshot()
	ticker := time.NewTicker(devPoll)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
		cur := s.devSnapshot()
		for path, mod := range cur {
			if !prev[path].Equal(mod) {
				if send(devEvent{Changed: path}) != nil {
					return
				}
			}
		}
		prev = cur
	}
}

// devSnapshot maps each template and static file to its modification time.
func (s *server) devSnapshot() map[string]time.Time {
	snap := map[string]time.Time{}
	for _, dir := range []string{"templates", "static"} {
		_ = fs.WalkDir(s.templates, dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil //nolint:nilerr // a file removed mid-walk is skipped
			}
			if info, err := d.Info(); err == nil {
				snap[path] = info.ModTime()
			}
			return nil
		})
	}
	return snap
}

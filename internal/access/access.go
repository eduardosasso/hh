// Package access reads the request log the proxy writes, one Caddy JSON object per line.
package access

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"time"
)

const (
	// LatencyKey is the field with the upstream response header latency, in seconds.
	LatencyKey = "app_latency"
	// ErrorKey is the field with the error Caddy recorded for the request.
	ErrorKey = "error"
)

// ErrLine marks a log line that is not a request record.
var ErrLine = errors.New("unreadable request log line")

// Entry is one request the proxy handled.
type Entry struct {
	Time     time.Time
	Host     string
	Method   string
	URI      string
	Proto    string
	Status   int
	Size     int
	Duration time.Duration
	// App is the upstream latency. Zero when no upstream answered.
	App      time.Duration
	Type     string
	Location string
	Error    string
	// Raw is the whole line, for callers that pass it through.
	Raw map[string]any
}

type record struct {
	TS      float64 `json:"ts"`
	Request struct {
		Host   string `json:"host"`
		Method string `json:"method"`
		URI    string `json:"uri"`
		Proto  string `json:"proto"`
	} `json:"request"`
	Duration float64     `json:"duration"`
	Size     int         `json:"size"`
	Status   int         `json:"status"`
	Headers  http.Header `json:"resp_headers"`
}

// Parse reads one access log line.
func Parse(line []byte) (Entry, error) {
	var r record
	if err := json.Unmarshal(line, &r); err != nil {
		return Entry{}, fmt.Errorf("%w: %w", ErrLine, err)
	}
	if r.Status == 0 || r.Request.Host == "" {
		return Entry{}, fmt.Errorf("%w: no status or host", ErrLine)
	}
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return Entry{}, fmt.Errorf("%w: %w", ErrLine, err)
	}
	latency, _ := raw[LatencyKey].(float64)
	reason, _ := raw[ErrorKey].(string)

	return Entry{
		Time:     unix(r.TS),
		Host:     bare(r.Request.Host),
		Method:   r.Request.Method,
		URI:      r.Request.URI,
		Proto:    r.Request.Proto,
		Status:   r.Status,
		Size:     r.Size,
		Duration: seconds(r.Duration),
		App:      seconds(latency),
		Type:     r.Headers.Get("Content-Type"),
		Location: r.Headers.Get("Location"),
		Error:    reason,
		Raw:      raw,
	}, nil
}

func unix(ts float64) time.Time {
	sec, frac := math.Modf(ts)

	return time.Unix(int64(sec), int64(frac*float64(time.Second)))
}

func seconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}

// bare drops the port from a Host header.
func bare(host string) string {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}

	return h
}

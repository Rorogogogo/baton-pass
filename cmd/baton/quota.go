package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type QuotaUsage struct {
	Claude ClaudeQuotaUsage `json:"claude"`
}

type ClaudeQuotaUsage struct {
	FiveHour UsageWindow  `json:"five_hour"`
	SevenDay *UsageWindow `json:"seven_day,omitempty"`
}

type UsageWindow struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       int64   `json:"resets_at"`
	UpdatedAt      int64   `json:"updated_at"`
}

type statuslinePayload struct {
	RateLimits struct {
		FiveHour *struct {
			UsedPercentage *float64 `json:"used_percentage"`
			ResetsAt       *int64   `json:"resets_at"`
		} `json:"five_hour"`
		SevenDay *struct {
			UsedPercentage *float64 `json:"used_percentage"`
			ResetsAt       *int64   `json:"resets_at"`
		} `json:"seven_day"`
	} `json:"rate_limits"`
}

func parseQuotaUsage(input []byte, now time.Time) (QuotaUsage, bool) {
	var payload statuslinePayload
	if json.Unmarshal(input, &payload) != nil || payload.RateLimits.FiveHour == nil {
		return QuotaUsage{}, false
	}
	five := payload.RateLimits.FiveHour
	if five.UsedPercentage == nil || five.ResetsAt == nil ||
		*five.UsedPercentage < 0 || *five.UsedPercentage > 100 || *five.ResetsAt <= 0 {
		return QuotaUsage{}, false
	}
	usage := QuotaUsage{Claude: ClaudeQuotaUsage{FiveHour: UsageWindow{
		UsedPercentage: *five.UsedPercentage, ResetsAt: *five.ResetsAt, UpdatedAt: now.Unix(),
	}}}
	if seven := payload.RateLimits.SevenDay; seven != nil && seven.UsedPercentage != nil && seven.ResetsAt != nil &&
		*seven.UsedPercentage >= 0 && *seven.UsedPercentage <= 100 && *seven.ResetsAt > 0 {
		usage.Claude.SevenDay = &UsageWindow{
			UsedPercentage: *seven.UsedPercentage, ResetsAt: *seven.ResetsAt, UpdatedAt: now.Unix(),
		}
	}
	return usage, true
}

func readQuotaUsage(now time.Time, staleAfter int64) (QuotaUsage, bool) {
	var usage QuotaUsage
	b, err := os.ReadFile(filepath.Join(dataDir(), "usage.json"))
	if err != nil || json.Unmarshal(b, &usage) != nil {
		return QuotaUsage{}, false
	}
	w := usage.Claude.FiveHour
	if w.UsedPercentage < 0 || w.UsedPercentage > 100 || w.ResetsAt <= 0 || w.UpdatedAt <= 0 ||
		now.Unix() >= w.ResetsAt || now.Unix()-w.UpdatedAt > staleAfter || w.UpdatedAt > now.Unix()+60 {
		return QuotaUsage{}, false
	}
	return usage, true
}

// statusline is a silent telemetry writer. When an existing status line was
// wrapped during installation, its base64-encoded shell command is executed
// with the exact original JSON on stdin and remains responsible for stdout.
func statusline(args []string) {
	input, err := io.ReadAll(io.LimitReader(os.Stdin, 4<<20))
	if err != nil {
		return
	}
	if usage, ok := parseQuotaUsage(input, time.Now()); ok {
		_ = writeJSONAtomic(filepath.Join(dataDir(), "usage.json"), usage)
	}
	if len(args) == 2 && args[0] == "--passthrough" {
		encoded, err := base64.RawURLEncoding.DecodeString(args[1])
		if err != nil || len(encoded) == 0 {
			return
		}
		cmd := exec.Command("/bin/sh", "-c", string(encoded))
		cmd.Stdin = bytes.NewReader(input)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}
}

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultContextThreshold = 190000
	defaultAwarePercent     = 75.0
	defaultCautionPercent   = 85.0
	defaultHandoffPercent   = 92.0
	defaultEmergencyPercent = 96.0
	defaultStaleAfter       = 1800
)

type Config struct {
	Context ContextConfig `json:"context"`
	Quota   QuotaConfig   `json:"quota"`
}

type ContextConfig struct {
	Enabled         bool `json:"enabled"`
	ThresholdTokens int  `json:"threshold_tokens"`
}

type QuotaConfig struct {
	Enabled           bool    `json:"enabled"`
	AwarePercent      float64 `json:"aware_percent"`
	CautionPercent    float64 `json:"caution_percent"`
	HandoffPercent    float64 `json:"handoff_percent"`
	EmergencyPercent  float64 `json:"emergency_percent"`
	StaleAfterSeconds int64   `json:"stale_after_seconds,omitempty"`
}

// legacyConfig preserves the original context guard and leaves quota off when
// an existing installation has no config.json yet.
func legacyConfig() Config {
	return Config{
		Context: ContextConfig{Enabled: true, ThresholdTokens: defaultContextThreshold},
		Quota: QuotaConfig{
			Enabled: false, AwarePercent: defaultAwarePercent,
			CautionPercent: defaultCautionPercent, HandoffPercent: defaultHandoffPercent,
			EmergencyPercent: defaultEmergencyPercent, StaleAfterSeconds: defaultStaleAfter,
		},
	}
}

func loadConfig() Config {
	cfg := legacyConfig()
	b, err := os.ReadFile(filepath.Join(dataDir(), "config.json"))
	if err != nil {
		return cfg
	}
	if json.Unmarshal(b, &cfg) != nil || validateConfig(cfg) != nil {
		return legacyConfig()
	}
	return cfg
}

func validateConfig(cfg Config) error {
	if cfg.Context.ThresholdTokens <= 0 {
		return fmt.Errorf("context threshold must be positive")
	}
	q := cfg.Quota
	if q.AwarePercent < 0 || q.EmergencyPercent > 100 ||
		q.AwarePercent > q.CautionPercent || q.CautionPercent > q.HandoffPercent ||
		q.HandoffPercent > q.EmergencyPercent || q.StaleAfterSeconds <= 0 {
		return fmt.Errorf("quota thresholds must be ordered from 0 to 100")
	}
	return nil
}

func saveConfig(cfg Config) error {
	if err := validateConfig(cfg); err != nil {
		return err
	}
	return writeJSONAtomic(filepath.Join(dataDir(), "config.json"), cfg)
}

func effectiveContextThreshold(cfg Config) int {
	return envInt("BATON_THRESHOLD", cfg.Context.ThresholdTokens)
}

func setGuard(name string, enabled bool) {
	cfg := loadConfig()
	switch name {
	case "context":
		cfg.Context.Enabled = enabled
	case "quota":
		cfg.Quota.Enabled = enabled
	default:
		fatalUsage("usage: baton {enable|disable} {context|quota}")
	}
	if err := saveConfig(cfg); err != nil {
		fatal("baton: %v", err)
	}
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	fmt.Printf("baton-pass: %s %s\n", name, state)
}

func configure(args []string) {
	cfg := loadConfig()
	if len(args) == 2 {
		value, err := strconv.ParseFloat(args[1], 64)
		if err != nil {
			fatal("baton: invalid value %q", args[1])
		}
		switch args[0] {
		case "quota":
			setHandoffThreshold(&cfg.Quota, value)
		case "context":
			cfg.Context.ThresholdTokens = int(value)
		default:
			fatalUsage("usage: baton config [context TOKENS|quota PERCENT]")
		}
		if err := saveConfig(cfg); err != nil {
			fatal("baton: %v", err)
		}
		showStatus()
		return
	}
	if len(args) != 0 {
		fatalUsage("usage: baton config [context TOKENS|quota PERCENT]")
	}

	in := bufio.NewReader(os.Stdin)
	fmt.Println("Baton Pass configuration")
	fmt.Printf("Enable Claude 5-hour quota guard? [%s]: ", yesNo(cfg.Quota.Enabled))
	cfg.Quota.Enabled = readBool(in, cfg.Quota.Enabled)
	fmt.Printf("Enable context guard? [%s]: ", yesNo(cfg.Context.Enabled))
	cfg.Context.Enabled = readBool(in, cfg.Context.Enabled)
	if cfg.Quota.Enabled {
		fmt.Printf("Quota handoff percentage [%.0f]: ", cfg.Quota.HandoffPercent)
		setHandoffThreshold(&cfg.Quota, readFloat(in, cfg.Quota.HandoffPercent))
	}
	if cfg.Context.Enabled {
		fmt.Printf("Context threshold tokens [%d]: ", cfg.Context.ThresholdTokens)
		cfg.Context.ThresholdTokens = int(readFloat(in, float64(cfg.Context.ThresholdTokens)))
	}
	if err := saveConfig(cfg); err != nil {
		fatal("baton: %v", err)
	}
	showStatus()
}

func initConfig(args []string) {
	cfg := legacyConfig()
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			fatalUsage("usage: baton init-config --context BOOL --quota BOOL [--quota-threshold N]")
		}
		key, value := args[i], args[i+1]
		i++
		switch key {
		case "--context":
			cfg.Context.Enabled = parseBoolArg(value)
		case "--quota":
			cfg.Quota.Enabled = parseBoolArg(value)
		case "--quota-threshold":
			v, err := strconv.ParseFloat(value, 64)
			if err != nil {
				fatal("baton: invalid quota threshold %q", value)
			}
			setHandoffThreshold(&cfg.Quota, v)
		default:
			fatalUsage("usage: baton init-config --context BOOL --quota BOOL [--quota-threshold N]")
		}
	}
	if err := saveConfig(cfg); err != nil {
		fatal("baton: %v", err)
	}
}

func parseBoolArg(value string) bool {
	switch value {
	case "true":
		return true
	case "false":
		return false
	default:
		fatal("baton: expected true or false, got %q", value)
		return false
	}
}

func setHandoffThreshold(quota *QuotaConfig, value float64) {
	quota.HandoffPercent = value
	if quota.AwarePercent > value {
		quota.AwarePercent = value
	}
	if quota.CautionPercent > value {
		quota.CautionPercent = value
	}
	if quota.EmergencyPercent < value {
		quota.EmergencyPercent = value
	}
}

func showStatus() {
	cfg := loadConfig()
	fmt.Println("Baton Pass")
	fmt.Println("\nHandoff\n  manual        enabled")
	fmt.Println("\nGuards")
	if cfg.Quota.Enabled {
		fmt.Printf("  quota         enabled · handoff at %.0f%%\n", cfg.Quota.HandoffPercent)
	} else {
		fmt.Println("  quota         disabled")
	}
	if cfg.Context.Enabled {
		fmt.Printf("  context       enabled · handoff at %s tokens\n", commas(effectiveContextThreshold(cfg)))
	} else {
		fmt.Println("  context       disabled")
	}
	fmt.Println("\nClaude usage")
	usage, ok := readQuotaUsage(time.Now(), cfg.Quota.StaleAfterSeconds)
	if !ok {
		fmt.Println("  5h            unavailable")
		return
	}
	fmt.Printf("  5h            %.0f%%\n", usage.Claude.FiveHour.UsedPercentage)
	fmt.Printf("  reset         %s\n", time.Unix(usage.Claude.FiveHour.ResetsAt, 0).Local().Format("3:04 PM"))
}

func yesNo(v bool) string {
	if v {
		return "Y/n"
	}
	return "y/N"
}

func readBool(in *bufio.Reader, def bool) bool {
	v, _ := in.ReadString('\n')
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return def
	}
	return v == "y" || v == "yes" || v == "true" || v == "1"
}

func readFloat(in *bufio.Reader, def float64) float64 {
	v, _ := in.ReadString('\n')
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return n
}

func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".baton-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(append(b, '\n')); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

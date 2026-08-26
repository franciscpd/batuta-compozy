package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/franciscpd/batuta-compozy/internal/publication"
)

const (
	loopConfigStdoutLimit int64 = 2 * 1024 * 1024
	loopConfigStderrLimit int64 = 64 * 1024
)

type LoopConfigDocument map[string]json.RawMessage

type LoopConfigSnapshot struct {
	Config          LoopConfigDocument
	RuntimeRules    []RuntimeRule
	EffectiveConfig json.RawMessage
	ConfigRevision  int64
}

type ConfigRevisionConflictError struct {
	ExpectedRevision int64
	CurrentRevision  int64
}

func (e *ConfigRevisionConflictError) Error() string {
	return fmt.Sprintf("routing: loop config revision conflict: expected %d, current %d", e.ExpectedRevision, e.CurrentRevision)
}

type LoopConfigClient struct {
	Executable string
	Runner     publication.CommandRunner
}

func (c LoopConfigClient) Read(ctx context.Context, workspaceID, loopName string) (LoopConfigSnapshot, error) {
	if err := c.validate(workspaceID, loopName); err != nil {
		return LoopConfigSnapshot{}, err
	}
	result, err := c.Runner.Run(ctx, publication.Command{
		Executable: c.Executable,
		Args: []string{
			"loop", "config", "--workspace", workspaceID, "--name", loopName, "-o", "json",
		},
		StdoutLimit: loopConfigStdoutLimit,
		StderrLimit: loopConfigStderrLimit,
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return LoopConfigSnapshot{}, ctxErr
		}
		return LoopConfigSnapshot{}, errors.New("routing: read loop config failed")
	}
	return decodeLoopConfigSnapshot(result.Stdout)
}

func (c LoopConfigClient) Write(ctx context.Context, workspaceID, loopName string, expectedRevision int64, config LoopConfigDocument) (LoopConfigSnapshot, error) {
	if err := c.validate(workspaceID, loopName); err != nil {
		return LoopConfigSnapshot{}, err
	}
	if expectedRevision < 0 {
		return LoopConfigSnapshot{}, errors.New("routing: expected revision must be non-negative")
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return LoopConfigSnapshot{}, errors.New("routing: encode loop config failed")
	}
	file, err := os.CreateTemp("", "batuta-loop-config-*.json")
	if err != nil {
		return LoopConfigSnapshot{}, errors.New("routing: create internal loop config failed")
	}
	path := file.Name()
	defer func() { _ = os.Remove(path) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return LoopConfigSnapshot{}, errors.New("routing: secure internal loop config failed")
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return LoopConfigSnapshot{}, errors.New("routing: write internal loop config failed")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return LoopConfigSnapshot{}, errors.New("routing: sync internal loop config failed")
	}
	if err := file.Close(); err != nil {
		return LoopConfigSnapshot{}, errors.New("routing: close internal loop config failed")
	}

	result, runErr := c.Runner.Run(ctx, publication.Command{
		Executable: c.Executable,
		Args: []string{
			"loop", "configure", "--workspace", workspaceID, "--name", loopName,
			"--expected-revision", strconv.FormatInt(expectedRevision, 10), "--file", path, "-o", "json",
		},
		StdoutLimit: loopConfigStdoutLimit,
		StderrLimit: loopConfigStderrLimit,
	})
	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return LoopConfigSnapshot{}, ctxErr
		}
		if conflict, ok := decodeConfigConflict(result.Stdout); ok {
			return LoopConfigSnapshot{}, conflict
		}
		return LoopConfigSnapshot{}, errors.New("routing: configure loop failed")
	}
	return decodeLoopConfigSnapshot(result.Stdout)
}

func (c LoopConfigClient) validate(workspaceID, loopName string) error {
	if strings.TrimSpace(c.Executable) == "" || !filepath.IsAbs(c.Executable) || c.Runner == nil {
		return errors.New("routing: fixed absolute Compozy command boundary is required")
	}
	if !boundedArgument(workspaceID) || !boundedArgument(loopName) {
		return errors.New("routing: trusted workspace and loop name are required")
	}
	return nil
}

func boundedArgument(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && trimmed == value && len(value) <= 256 && !strings.ContainsAny(value, "\x00\r\n")
}

func decodeLoopConfigSnapshot(payload []byte) (LoopConfigSnapshot, error) {
	var wire struct {
		Config          json.RawMessage `json:"config"`
		EffectiveConfig json.RawMessage `json:"effective_config"`
		ConfigRevision  *int64          `json:"config_revision"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return LoopConfigSnapshot{}, errors.New("routing: malformed loop config response")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return LoopConfigSnapshot{}, errors.New("routing: malformed loop config response")
	}
	if wire.ConfigRevision == nil || *wire.ConfigRevision < 0 || len(wire.Config) == 0 || len(wire.EffectiveConfig) == 0 || !json.Valid(wire.EffectiveConfig) {
		return LoopConfigSnapshot{}, errors.New("routing: malformed loop config response")
	}
	config := make(LoopConfigDocument)
	if string(wire.Config) != "null" {
		if err := json.Unmarshal(wire.Config, &config); err != nil || config == nil {
			return LoopConfigSnapshot{}, errors.New("routing: malformed loop config response")
		}
	}
	rules, err := decodeRuntimeRules(config["runtime_rules"])
	if err != nil {
		return LoopConfigSnapshot{}, err
	}
	return LoopConfigSnapshot{
		Config: cloneLoopConfig(config), RuntimeRules: rules,
		EffectiveConfig: append(json.RawMessage(nil), wire.EffectiveConfig...), ConfigRevision: *wire.ConfigRevision,
	}, nil
}

func decodeConfigConflict(payload []byte) (*ConfigRevisionConflictError, bool) {
	var wire struct {
		Error            string `json:"error"`
		ExpectedRevision *int64 `json:"expected_revision"`
		CurrentRevision  *int64 `json:"current_revision"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&wire) != nil || wire.ExpectedRevision == nil || wire.CurrentRevision == nil || *wire.ExpectedRevision < 0 || *wire.CurrentRevision < 0 || strings.TrimSpace(wire.Error) == "" {
		return nil, false
	}
	return &ConfigRevisionConflictError{ExpectedRevision: *wire.ExpectedRevision, CurrentRevision: *wire.CurrentRevision}, true
}

func MergeRuntimeRules(config LoopConfigDocument, previousOwned, nextOwned []RuntimeRule) (LoopConfigDocument, error) {
	merged := cloneLoopConfig(config)
	existing, err := decodeRuntimeRules(merged["runtime_rules"])
	if err != nil {
		return nil, err
	}
	ownedCounts := make(map[string]int, len(previousOwned))
	for _, rule := range previousOwned {
		fingerprint, err := ruleFingerprint(rule)
		if err != nil {
			return nil, err
		}
		ownedCounts[fingerprint]++
	}
	retained := make([]RuntimeRule, 0, len(existing)+len(nextOwned))
	for _, rule := range existing {
		fingerprint, err := ruleFingerprint(rule)
		if err != nil {
			return nil, err
		}
		if ownedCounts[fingerprint] > 0 {
			ownedCounts[fingerprint]--
			continue
		}
		retained = append(retained, rule)
	}
	for _, count := range ownedCounts {
		if count != 0 {
			return nil, errors.New("routing: matrix ownership cannot be proven")
		}
	}
	retained = append(retained, nextOwned...)
	payload, err := json.Marshal(retained)
	if err != nil {
		return nil, errors.New("routing: encode merged runtime rules failed")
	}
	merged["runtime_rules"] = payload
	return merged, nil
}

func decodeRuntimeRules(payload json.RawMessage) ([]RuntimeRule, error) {
	if len(payload) == 0 || string(payload) == "null" {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var rules []RuntimeRule
	if err := decoder.Decode(&rules); err != nil {
		return nil, errors.New("routing: stored runtime rules are malformed")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("routing: stored runtime rules are malformed")
	}
	if rules == nil {
		return nil, errors.New("routing: stored runtime rules are malformed")
	}
	return rules, nil
}

func ruleFingerprint(rule RuntimeRule) (string, error) {
	payload, err := json.Marshal(rule)
	if err != nil {
		return "", errors.New("routing: fingerprint runtime rule failed")
	}
	return string(payload), nil
}

func cloneLoopConfig(config LoopConfigDocument) LoopConfigDocument {
	cloned := make(LoopConfigDocument, len(config))
	for key, value := range config {
		cloned[key] = append(json.RawMessage(nil), value...)
	}
	return cloned
}

package routing

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const journalSchemaVersion = 1

var (
	ErrOwnershipUnproven       = errors.New("routing: matrix ownership cannot be proven")
	ErrDeliveryBindingConflict = errors.New("routing: delivery generation binding conflict")
	ErrDeliveryLivenessUnknown = errors.New("routing: delivery liveness is unknown")
)

type OwnedRuntimeRule struct {
	Rule        RuntimeRule `json:"rule"`
	Fingerprint string      `json:"fingerprint"`
}

type RoutingJournal struct {
	SchemaVersion     int                          `json:"schema_version"`
	CurrentGeneration string                       `json:"current_generation,omitempty"`
	Generations       map[string]RoutingGeneration `json:"generations"`
	DeliveryBindings  map[string]string            `json:"delivery_bindings"`
	OwnedRules        []OwnedRuntimeRule           `json:"owned_rules,omitempty"`
}

type DeliveryLiveness string

const (
	DeliveryLive     DeliveryLiveness = "live"
	DeliveryTerminal DeliveryLiveness = "terminal"
	DeliveryUnknown  DeliveryLiveness = "unknown"
)

type OwnershipStore struct {
	root string
	mu   sync.Mutex
}

func NewOwnershipStore(root string) (*OwnershipStore, error) {
	if strings.TrimSpace(root) == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return nil, errors.New("routing: user cache directory is unavailable")
		}
		root = filepath.Join(cache, "batuta", "routing", "v1")
	}
	if !filepath.IsAbs(root) {
		return nil, errors.New("routing: ownership root must be absolute")
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, errors.New("routing: create ownership directory failed")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, errors.New("routing: secure ownership directory failed")
	}
	return &OwnershipStore{root: root}, nil
}

func (s *OwnershipStore) pathFor(workspaceID string) string {
	digest := sha256.Sum256([]byte(workspaceID))
	return filepath.Join(s.root, hex.EncodeToString(digest[:])+".json")
}

func (s *OwnershipStore) Load(workspaceID string) (RoutingJournal, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(workspaceID)
}

func (s *OwnershipStore) Save(workspaceID string, journal RoutingJournal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(workspaceID, journal)
}

func (s *OwnershipStore) BindDelivery(workspaceID, runID, generationDigest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	journal, exists, err := s.load(workspaceID)
	if err != nil || !exists {
		if err != nil {
			return err
		}
		return ErrOwnershipUnproven
	}
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(generationDigest) == "" {
		return ErrDeliveryBindingConflict
	}
	if _, exists := journal.Generations[generationDigest]; !exists {
		return ErrDeliveryBindingConflict
	}
	if current := journal.DeliveryBindings[runID]; current != "" && current != generationDigest {
		return ErrDeliveryBindingConflict
	}
	journal.DeliveryBindings[runID] = generationDigest
	return s.save(workspaceID, journal)
}

func (s *OwnershipStore) GenerationForDelivery(workspaceID, runID, authoritativeDigest string) (RoutingGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	journal, exists, err := s.load(workspaceID)
	if err != nil || !exists {
		if err != nil {
			return RoutingGeneration{}, err
		}
		return RoutingGeneration{}, ErrOwnershipUnproven
	}
	generation, exists := journal.Generations[authoritativeDigest]
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(authoritativeDigest) == "" || !exists {
		return RoutingGeneration{}, ErrDeliveryBindingConflict
	}
	bound := journal.DeliveryBindings[runID]
	if bound != "" && bound != authoritativeDigest {
		return RoutingGeneration{}, ErrDeliveryBindingConflict
	}
	if bound == "" {
		journal.DeliveryBindings[runID] = authoritativeDigest
		if err := s.save(workspaceID, journal); err != nil {
			return RoutingGeneration{}, err
		}
	}
	return generation, nil
}

func (s *OwnershipStore) Prune(workspaceID string, states map[string]DeliveryLiveness) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	journal, exists, err := s.load(workspaceID)
	if err != nil || !exists {
		if err != nil {
			return err
		}
		return ErrOwnershipUnproven
	}
	for runID := range journal.DeliveryBindings {
		state, exists := states[runID]
		if !exists || state == DeliveryUnknown {
			return ErrDeliveryLivenessUnknown
		}
		if state != DeliveryLive && state != DeliveryTerminal {
			return ErrDeliveryLivenessUnknown
		}
	}
	for runID := range journal.DeliveryBindings {
		if states[runID] == DeliveryTerminal {
			delete(journal.DeliveryBindings, runID)
		}
	}
	referenced := make(map[string]struct{}, len(journal.DeliveryBindings)+1)
	referenced[journal.CurrentGeneration] = struct{}{}
	for _, digest := range journal.DeliveryBindings {
		referenced[digest] = struct{}{}
	}
	for digest := range journal.Generations {
		if _, keep := referenced[digest]; !keep {
			delete(journal.Generations, digest)
		}
	}
	return s.save(workspaceID, journal)
}

func (s *OwnershipStore) load(workspaceID string) (RoutingJournal, bool, error) {
	if !boundedArgument(workspaceID) {
		return RoutingJournal{}, false, errors.New("routing: trusted workspace ID is required")
	}
	file, err := os.Open(s.pathFor(workspaceID))
	if errors.Is(err, os.ErrNotExist) {
		return RoutingJournal{}, false, nil
	}
	if err != nil {
		return RoutingJournal{}, false, errors.New("routing: read ownership journal failed")
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, 16*1024*1024+1))
	if err != nil || len(payload) > 16*1024*1024 {
		return RoutingJournal{}, false, errors.New("routing: ownership journal is unavailable")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var journal RoutingJournal
	if err := decoder.Decode(&journal); err != nil {
		return RoutingJournal{}, false, ErrOwnershipUnproven
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return RoutingJournal{}, false, ErrOwnershipUnproven
	}
	if err := validateJournal(journal); err != nil {
		return RoutingJournal{}, false, err
	}
	return journal, true, nil
}

func (s *OwnershipStore) save(workspaceID string, journal RoutingJournal) error {
	if !boundedArgument(workspaceID) {
		return errors.New("routing: trusted workspace ID is required")
	}
	if err := validateJournal(journal); err != nil {
		return err
	}
	payload, err := json.Marshal(journal)
	if err != nil {
		return errors.New("routing: encode ownership journal failed")
	}
	file, err := os.CreateTemp(s.root, ".routing-journal-*.tmp")
	if err != nil {
		return errors.New("routing: create ownership journal failed")
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return errors.New("routing: secure ownership journal failed")
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return errors.New("routing: write ownership journal failed")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return errors.New("routing: sync ownership journal failed")
	}
	if err := file.Close(); err != nil {
		return errors.New("routing: close ownership journal failed")
	}
	if err := os.Rename(temporary, s.pathFor(workspaceID)); err != nil {
		return errors.New("routing: replace ownership journal failed")
	}
	if err := os.Chmod(s.pathFor(workspaceID), 0o600); err != nil {
		return errors.New("routing: secure ownership journal failed")
	}
	return nil
}

func validateJournal(journal RoutingJournal) error {
	if journal.SchemaVersion != journalSchemaVersion || journal.Generations == nil || journal.DeliveryBindings == nil {
		return ErrOwnershipUnproven
	}
	if journal.CurrentGeneration != "" {
		if generation, exists := journal.Generations[journal.CurrentGeneration]; !exists || generation.Digest != journal.CurrentGeneration {
			return ErrOwnershipUnproven
		}
	}
	for digest, generation := range journal.Generations {
		if strings.TrimSpace(digest) == "" || generation.Digest != digest {
			return ErrOwnershipUnproven
		}
	}
	for runID, digest := range journal.DeliveryBindings {
		if strings.TrimSpace(runID) == "" {
			return ErrOwnershipUnproven
		}
		if _, exists := journal.Generations[digest]; !exists {
			return ErrOwnershipUnproven
		}
	}
	for _, owned := range journal.OwnedRules {
		fingerprint, err := ruleFingerprint(owned.Rule)
		if err != nil || owned.Fingerprint != fingerprint {
			return fmt.Errorf("%w: owned rule fingerprint mismatch", ErrOwnershipUnproven)
		}
	}
	return nil
}

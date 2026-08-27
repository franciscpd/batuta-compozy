package routing

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
)

const (
	journalSchemaVersion   = 2
	maxRoutingJournalBytes = 16 << 20
)

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
	CurrentGeneration string                       `json:"current_generation"`
	Generations       map[string]RoutingGeneration `json:"generations"`
	Deliveries        map[string]DeliveryRecord    `json:"deliveries"`

	// Compatibility-only fields for unreleased v1 callers. Schema v2 never
	// encodes them, and the v1 reader intentionally discards them.
	DeliveryBindings map[string]string  `json:"-"`
	OwnedRules       []OwnedRuntimeRule `json:"-"`
}

type routingJournalV1 struct {
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

type JournalTx struct {
	Journal *RoutingJournal
	persist func() error
}

func (tx *JournalTx) Persist() error {
	if tx == nil || tx.Journal == nil || tx.persist == nil {
		return ErrOwnershipUnproven
	}
	return tx.persist()
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
	var journal RoutingJournal
	var exists bool
	err := s.withWorkspaceLock(workspaceID, func() error {
		var err error
		journal, exists, err = s.load(workspaceID)
		return err
	})
	return journal, exists, err
}

func (s *OwnershipStore) WithLockedJournal(workspaceID string, fn func(*JournalTx) error) error {
	if fn == nil {
		return errors.New("routing: journal action is required")
	}
	return s.withWorkspaceLock(workspaceID, func() error {
		journal, exists, err := s.load(workspaceID)
		if err != nil {
			return err
		}
		if !exists {
			journal = emptyRoutingJournal()
		}
		original, err := cloneJournal(journal)
		if err != nil {
			return err
		}
		tx := &JournalTx{Journal: &journal}
		tx.persist = func() error {
			if err := validateJournalTransition(original, journal); err != nil {
				return err
			}
			if err := s.save(workspaceID, journal); err != nil {
				return err
			}
			original, err = cloneJournal(journal)
			return err
		}
		return fn(tx)
	})
}

// Save remains temporarily for callers being replaced by the migration-free
// matrix and recovery tasks. New code mutates through WithLockedJournal.
func (s *OwnershipStore) Save(workspaceID string, journal RoutingJournal) error {
	return s.withWorkspaceLock(workspaceID, func() error { return s.save(workspaceID, journal) })
}

// GenerationForDelivery remains temporarily for the old same-lineage recovery
// owner. The migration-free recovery task replaces this caller.
func (s *OwnershipStore) GenerationForDelivery(_, _, _ string) (RoutingGeneration, error) {
	return RoutingGeneration{}, ErrDeliveryBindingConflict
}

func (s *OwnershipStore) withWorkspaceLock(workspaceID string, action func() error) error {
	if !boundedArgument(workspaceID) || action == nil {
		return errors.New("routing: trusted workspace ID and journal action are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lockFile, err := os.OpenFile(s.pathFor(workspaceID)+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return errors.New("routing: open ownership journal lock failed")
	}
	defer lockFile.Close()
	if err := lockFile.Chmod(0o600); err != nil {
		return errors.New("routing: secure ownership journal lock failed")
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return errors.New("routing: lock ownership journal failed")
	}
	defer func() { _ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) }()
	return action()
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
	payload, err := io.ReadAll(io.LimitReader(file, maxRoutingJournalBytes+1))
	if err != nil || len(payload) > maxRoutingJournalBytes {
		return RoutingJournal{}, false, ErrOwnershipUnproven
	}
	var envelope struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return RoutingJournal{}, false, ErrOwnershipUnproven
	}
	var journal RoutingJournal
	switch envelope.SchemaVersion {
	case 1:
		legacy, err := decodeJournalV1(payload)
		if err != nil {
			return RoutingJournal{}, false, err
		}
		journal = RoutingJournal{
			SchemaVersion: journalSchemaVersion, CurrentGeneration: legacy.CurrentGeneration,
			Generations: legacy.Generations, Deliveries: map[string]DeliveryRecord{},
		}
	case journalSchemaVersion:
		if err := decodeStrictJSON(payload, &journal); err != nil {
			return RoutingJournal{}, false, ErrOwnershipUnproven
		}
	default:
		return RoutingJournal{}, false, ErrOwnershipUnproven
	}
	if err := validateJournal(journal); err != nil {
		return RoutingJournal{}, false, err
	}
	return journal, true, nil
}

func decodeJournalV1(payload []byte) (routingJournalV1, error) {
	var legacy routingJournalV1
	if err := decodeStrictJSON(payload, &legacy); err != nil || legacy.SchemaVersion != 1 || legacy.Generations == nil || legacy.DeliveryBindings == nil {
		return routingJournalV1{}, ErrOwnershipUnproven
	}
	if err := validateGenerationArchive(legacy.CurrentGeneration, legacy.Generations); err != nil {
		return routingJournalV1{}, err
	}
	for runID, digest := range legacy.DeliveryBindings {
		if !boundedArgument(runID) {
			return routingJournalV1{}, ErrOwnershipUnproven
		}
		if _, exists := legacy.Generations[digest]; !exists {
			return routingJournalV1{}, ErrOwnershipUnproven
		}
	}
	for _, owned := range legacy.OwnedRules {
		fingerprint, err := ruleFingerprint(owned.Rule)
		if err != nil || owned.Fingerprint != fingerprint {
			return routingJournalV1{}, ErrOwnershipUnproven
		}
	}
	return legacy, nil
}

func decodeStrictJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrOwnershipUnproven
	}
	return nil
}

func ruleFingerprint(rule RuntimeRule) (string, error) {
	payload, err := json.Marshal(rule)
	if err != nil {
		return "", errors.New("routing: fingerprint runtime rule failed")
	}
	return string(payload), nil
}

func (s *OwnershipStore) save(workspaceID string, journal RoutingJournal) error {
	if !boundedArgument(workspaceID) {
		return errors.New("routing: trusted workspace ID is required")
	}
	if err := validateJournal(journal); err != nil {
		return err
	}
	payload, err := json.Marshal(journal)
	if err != nil || len(payload) > maxRoutingJournalBytes {
		return ErrOwnershipUnproven
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
	path := s.pathFor(workspaceID)
	if err := os.Rename(temporary, path); err != nil {
		return errors.New("routing: replace ownership journal failed")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return errors.New("routing: secure ownership journal failed")
	}
	directory, err := os.Open(s.root)
	if err != nil {
		return errors.New("routing: open ownership directory failed")
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return errors.New("routing: sync ownership directory failed")
	}
	return nil
}

func validateJournal(journal RoutingJournal) error {
	if journal.SchemaVersion != journalSchemaVersion || journal.Generations == nil || journal.Deliveries == nil {
		return ErrOwnershipUnproven
	}
	if err := validateGenerationArchive(journal.CurrentGeneration, journal.Generations); err != nil {
		return err
	}
	for deliveryID, delivery := range journal.Deliveries {
		if deliveryID != delivery.DeliveryID {
			return ErrOwnershipUnproven
		}
		if err := validateDelivery(delivery, journal.Generations); err != nil {
			return err
		}
	}
	return nil
}

func validateGenerationArchive(current string, generations map[string]RoutingGeneration) error {
	if current != "" {
		if generation, exists := generations[current]; !exists || generation.Digest != current {
			return ErrOwnershipUnproven
		}
	}
	for digest, generation := range generations {
		if !canonicalSHA256.MatchString(digest) || generation.Digest != digest {
			return ErrOwnershipUnproven
		}
		recomputed, err := finalizeGeneration(generation)
		if err != nil || recomputed.Digest != digest {
			return ErrOwnershipUnproven
		}
	}
	return nil
}

func validateJournalTransition(before, after RoutingJournal) error {
	if err := validateJournal(after); err != nil {
		if errors.Is(err, ErrInvalidDeliveryGraph) {
			for deliveryID, delivery := range before.Deliveries {
				candidate, exists := after.Deliveries[deliveryID]
				if !exists {
					return ErrDeliveryConflict
				}
				if transitionErr := validateDeliveryTransition(delivery, candidate); transitionErr != nil {
					return transitionErr
				}
			}
		}
		return err
	}
	for digest, generation := range before.Generations {
		if candidate, exists := after.Generations[digest]; !exists || !reflect.DeepEqual(generation, candidate) {
			return ErrDeliveryConflict
		}
	}
	for deliveryID, delivery := range before.Deliveries {
		candidate, exists := after.Deliveries[deliveryID]
		if !exists {
			return ErrDeliveryConflict
		}
		if err := validateDeliveryTransition(delivery, candidate); err != nil {
			return err
		}
	}
	return nil
}

func emptyRoutingJournal() RoutingJournal {
	return RoutingJournal{SchemaVersion: journalSchemaVersion, Generations: map[string]RoutingGeneration{}, Deliveries: map[string]DeliveryRecord{}}
}

func cloneJournal(journal RoutingJournal) (RoutingJournal, error) {
	payload, err := json.Marshal(journal)
	if err != nil {
		return RoutingJournal{}, ErrOwnershipUnproven
	}
	var cloned RoutingJournal
	if err := json.Unmarshal(payload, &cloned); err != nil {
		return RoutingJournal{}, ErrOwnershipUnproven
	}
	return cloned, nil
}

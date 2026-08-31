package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"time"
)

var ErrRoutingAlignmentRequired = errors.New("routing: current operator alignment is required")

type AlignmentState string

const (
	AlignmentRequired  AlignmentState = "required"
	AlignmentConfirmed AlignmentState = "confirmed"
)

type AlignmentRecord struct {
	AlignmentDigest  string        `json:"alignment_digest"`
	GenerationDigest string        `json:"generation_digest"`
	ConfirmedBy      string        `json:"confirmed_by"`
	ConfirmedAt      time.Time     `json:"confirmed_at"`
	Cells            []RoutingCell `json:"cells"`
}

type AlignmentStatus struct {
	State            AlignmentState `json:"state"`
	AlignmentDigest  string         `json:"alignment_digest"`
	GenerationDigest string         `json:"generation_digest"`
	ConfirmedBy      string         `json:"confirmed_by,omitempty"`
	ConfirmedAt      time.Time      `json:"confirmed_at,omitempty"`
	ChangedCells     []string       `json:"changed_cells,omitempty"`
	Replayed         bool           `json:"replayed"`
}

type AlignmentManager struct {
	Store *OwnershipStore
}

func (m AlignmentManager) Status(workspaceID string, generation RoutingGeneration) (AlignmentStatus, error) {
	digest, cells, err := alignmentIdentity(generation)
	if err != nil || m.Store == nil || !boundedArgument(workspaceID) {
		return AlignmentStatus{}, ErrOwnershipUnproven
	}
	status := AlignmentStatus{State: AlignmentRequired, AlignmentDigest: digest, GenerationDigest: generation.Digest}
	journal, exists, err := m.Store.Load(workspaceID)
	if err != nil || !exists {
		return status, err
	}
	if record, confirmed := journal.Alignments[digest]; confirmed {
		return alignmentStatus(record, generation.Digest, false), nil
	}
	latest, found := latestAlignment(journal.Alignments)
	if found {
		status.ChangedCells = changedAlignmentCells(latest.Cells, cells)
	}
	return status, nil
}

func (m AlignmentManager) Confirm(workspaceID, actorID string, generation RoutingGeneration) (AlignmentStatus, error) {
	digest, cells, err := alignmentIdentity(generation)
	if err != nil || m.Store == nil || !boundedArgument(workspaceID) || !boundedArgument(actorID) {
		return AlignmentStatus{}, ErrOwnershipUnproven
	}
	var result AlignmentStatus
	err = m.Store.WithLockedJournal(workspaceID, func(tx *JournalTx) error {
		archived, exists := tx.Journal.Generations[generation.Digest]
		if !exists {
			return ErrGenerationUnknown
		}
		if archived.Digest != generation.Digest {
			return ErrOwnershipUnproven
		}
		if tx.Journal.Alignments == nil {
			tx.Journal.Alignments = map[string]AlignmentRecord{}
		}
		if existing, exists := tx.Journal.Alignments[digest]; exists {
			if existing.GenerationDigest != generation.Digest {
				existing.GenerationDigest = generation.Digest
				tx.Journal.Alignments[digest] = existing
				if err := tx.Persist(); err != nil {
					return err
				}
			}
			result = alignmentStatus(existing, generation.Digest, true)
			return nil
		}
		record := AlignmentRecord{
			AlignmentDigest: digest, GenerationDigest: generation.Digest, ConfirmedBy: actorID,
			ConfirmedAt: time.Now().UTC(), Cells: cells,
		}
		tx.Journal.Alignments[digest] = record
		if err := tx.Persist(); err != nil {
			return err
		}
		result = alignmentStatus(record, generation.Digest, false)
		return nil
	})
	if err != nil {
		return AlignmentStatus{}, err
	}
	return result, nil
}

func alignmentIdentity(generation RoutingGeneration) (string, []RoutingCell, error) {
	recomputed, err := finalizeGeneration(generation)
	if err != nil || recomputed.Digest != generation.Digest || len(generation.Cells) == 0 {
		return "", nil, ErrOwnershipUnproven
	}
	cells := cloneAlignmentCells(generation.Cells)
	payload, err := json.Marshal(cells)
	if err != nil {
		return "", nil, ErrOwnershipUnproven
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), cells, nil
}

func validateAlignmentRecord(record AlignmentRecord) error {
	if !canonicalSHA256.MatchString(record.AlignmentDigest) || !canonicalSHA256.MatchString(record.GenerationDigest) ||
		!boundedArgument(record.ConfirmedBy) || record.ConfirmedAt.IsZero() || len(record.Cells) == 0 {
		return ErrOwnershipUnproven
	}
	payload, err := json.Marshal(record.Cells)
	if err != nil {
		return ErrOwnershipUnproven
	}
	digest := sha256.Sum256(payload)
	if record.AlignmentDigest != "sha256:"+hex.EncodeToString(digest[:]) {
		return ErrOwnershipUnproven
	}
	return nil
}

func alignmentStatus(record AlignmentRecord, generationDigest string, replayed bool) AlignmentStatus {
	return AlignmentStatus{
		State: AlignmentConfirmed, AlignmentDigest: record.AlignmentDigest, GenerationDigest: generationDigest,
		ConfirmedBy: record.ConfirmedBy, ConfirmedAt: record.ConfirmedAt, Replayed: replayed,
	}
}

func latestAlignment(records map[string]AlignmentRecord) (AlignmentRecord, bool) {
	var latest AlignmentRecord
	found := false
	for _, record := range records {
		if !found || record.ConfirmedAt.After(latest.ConfirmedAt) ||
			(record.ConfirmedAt.Equal(latest.ConfirmedAt) && strings.Compare(record.AlignmentDigest, latest.AlignmentDigest) > 0) {
			latest, found = record, true
		}
	}
	return latest, found
}

func changedAlignmentCells(before, after []RoutingCell) []string {
	previous := make(map[string]RoutingCell, len(before))
	for _, cell := range before {
		previous[alignmentCellKey(cell)] = cell
	}
	changed := make([]string, 0)
	for _, cell := range after {
		key := alignmentCellKey(cell)
		prior, exists := previous[key]
		if !exists || !reflect.DeepEqual(prior, cell) {
			changed = append(changed, key)
		}
		delete(previous, key)
	}
	for key := range previous {
		changed = append(changed, key)
	}
	slices.Sort(changed)
	return changed
}

func alignmentCellKey(cell RoutingCell) string {
	return string(cell.Domain) + "/" + string(cell.Complexity)
}

func cloneAlignmentCells(cells []RoutingCell) []RoutingCell {
	payload, _ := json.Marshal(cells)
	var cloned []RoutingCell
	_ = json.Unmarshal(payload, &cloned)
	return cloned
}

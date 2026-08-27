package worktreeops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/franciscpd/batuta-compozy/internal/publication"
)

const maxTaskExecutions = 4

var ErrInvalidWorktreeIdentity = errors.New("worktreeops: invalid worktree identity")

type Client interface {
	Create(context.Context, publication.TrustedScope, CreateRequest) (Worktree, error)
	FindByName(context.Context, publication.TrustedScope, string) (Worktree, bool, error)
	Inspect(context.Context, publication.TrustedScope, string) (Worktree, error)
	Remove(context.Context, publication.TrustedScope, string) (Worktree, error)
}

type CreateRequest struct {
	Name    string `json:"name"`
	Branch  string `json:"branch"`
	BaseSHA string `json:"base_sha"`
}

type SetupResult struct {
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

type Worktree struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	Root               string      `json:"root"`
	WorkspaceID        string      `json:"workspace_id"`
	RepositoryRoot     string      `json:"repository_root"`
	RepositoryIdentity string      `json:"repository_identity"`
	Branch             string      `json:"branch"`
	BaseRef            string      `json:"base_ref"`
	BaseSHA            string      `json:"base_sha"`
	State              string      `json:"state"`
	Setup              SetupResult `json:"setup"`
}

type IdentityInput struct {
	WorkspaceID string `json:"workspace_id"`
	DeliveryID  string `json:"delivery_id"`
	Wave        int    `json:"wave"`
	Slug        string `json:"slug"`
	TaskID      string `json:"task_id"`
	Execution   int    `json:"execution"`
	BaseSHA     string `json:"base_sha"`
}

type Identity struct {
	TaskID        string `json:"task_id"`
	Name          string `json:"name"`
	Branch        string `json:"branch"`
	OperationID   string `json:"operation_id"`
	RequestDigest string `json:"request_digest"`
}

func DeriveIdentity(input IdentityInput) (Identity, error) {
	deliveryDigest := strings.TrimPrefix(input.DeliveryID, "sha256:")
	slug := canonicalWorktreeSegment(input.Slug)
	task := canonicalWorktreeSegment(input.TaskID)
	if !boundedIdentity(input.WorkspaceID) || len(deliveryDigest) != 64 || !lowerHex(deliveryDigest) ||
		input.Wave < 1 || slug == "" || task == "" || input.Execution < 1 ||
		input.Execution > maxTaskExecutions || !gitSHA(input.BaseSHA) {
		return Identity{}, ErrInvalidWorktreeIdentity
	}
	seed, err := json.Marshal(input)
	if err != nil {
		return Identity{}, fmt.Errorf("worktreeops: encode identity seed: %w", err)
	}
	digest := sha256.Sum256(seed)
	suffix := hex.EncodeToString(digest[:4])
	identity := Identity{
		TaskID: input.TaskID,
		Name:   fmt.Sprintf("batuta-%s-%s-a%d-%s", slug, task, input.Execution, suffix),
		Branch: fmt.Sprintf("batuta/task/%s/%s/a%d-%s", deliveryDigest[:12], task, input.Execution, suffix),
	}
	request := struct {
		IdentityInput
		Name   string `json:"name"`
		Branch string `json:"branch"`
	}{IdentityInput: input, Name: identity.Name, Branch: identity.Branch}
	payload, err := json.Marshal(request)
	if err != nil {
		return Identity{}, fmt.Errorf("worktreeops: encode operation identity: %w", err)
	}
	requestDigest := sha256.Sum256(payload)
	identity.RequestDigest = "sha256:" + hex.EncodeToString(requestDigest[:])
	operationDigest := sha256.Sum256(append([]byte("worktree.create\x00"), payload...))
	identity.OperationID = "sha256:" + hex.EncodeToString(operationDigest[:])
	return identity, nil
}

func canonicalWorktreeSegment(value string) string {
	var output strings.Builder
	lastDash := false
	for _, current := range strings.ToLower(strings.TrimSpace(value)) {
		if current >= 'a' && current <= 'z' || current >= '0' && current <= '9' {
			output.WriteRune(current)
			lastDash = false
			continue
		}
		if !lastDash && output.Len() > 0 && current != 0 {
			output.WriteByte('-')
			lastDash = true
		}
	}
	canonical := strings.Trim(output.String(), "-")
	if len(canonical) > 32 {
		canonical = strings.TrimRight(canonical[:32], "-")
	}
	return canonical
}

func boundedIdentity(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 256 &&
		!strings.ContainsRune(value, '\x00')
}

func lowerHex(value string) bool {
	for _, current := range value {
		if current < '0' || current > '9' {
			if current < 'a' || current > 'f' {
				return false
			}
		}
	}
	return true
}

func gitSHA(value string) bool {
	return (len(value) == 40 || len(value) == 64) && lowerHex(value)
}

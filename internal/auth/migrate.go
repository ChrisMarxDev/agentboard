package auth

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/christophermarx/agentboard/internal/invitations"
)

// BootstrapFirstAdmin is the init-mints-invite flow. If no users exist:
//
//  1. (optional) Seeds a `@legacy-agent` user + token when legacyToken
//     is non-empty — backcompat for the deprecated AGENTBOARD_AUTH_TOKEN
//     env var. Kind=member (renamed from the v0 `agent` semantics).
//     When this runs, NO bootstrap invite is minted — the operator
//     already has an identity they can curl against.
//  2. Otherwise mints (or reuses) a role=admin invitation so the
//     operator can claim the first admin account via the web at
//     /invite/<id>.
//
// Returns the active bootstrap invitation when one was minted/reused.
// Callers use its ID to build the redeem URL for printing + writing
// to disk. Returns nil, nil when a user already exists OR when the
// legacy-agent seeding path ran.
//
// Idempotent:
//   - If any user already exists, returns nil, nil.
//   - If an active (unredeemed, unrevoked, unexpired) bootstrap invite
//     already exists, returns it unchanged.
//   - If a previous bootstrap invite expired, mints a fresh one.
//
// Pass expiresIn=0 to use the default 24h bootstrap window. Regular
// invites default to 7 days; bootstrap is shorter to force operators
// to complete the claim promptly.
func (s *Store) BootstrapFirstAdmin(
	invStore *invitations.Store,
	legacyToken string,
	expiresIn time.Duration,
	logger *log.Logger,
) (*invitations.Invitation, error) {
	has, err := s.HasAnyUser()
	if err != nil {
		return nil, fmt.Errorf("check users: %w", err)
	}
	if has {
		return nil, nil
	}
	if expiresIn <= 0 {
		expiresIn = 24 * time.Hour
	}

	// Seed @legacy-agent if the env var token is set.
	if legacyToken != "" {
		if _, err := s.GetUser("legacy-agent"); errors.Is(err, ErrNotFound) {
			if _, err := s.CreateUser(CreateUserParams{
				Username:  "legacy-agent",
				Kind:      KindMember,
				CreatedBy: invitations.BootstrapCreator,
			}); err != nil {
				return nil, fmt.Errorf("create legacy agent: %w", err)
			}
			if _, err := s.CreateToken(CreateTokenParams{
				Username:  "legacy-agent",
				TokenHash: HashToken(legacyToken),
				Label:     "legacy",
				CreatedBy: invitations.BootstrapCreator,
			}); err != nil {
				return nil, fmt.Errorf("create legacy token: %w", err)
			}
			if logger != nil {
				logger.Printf("Auth: mapped AGENTBOARD_AUTH_TOKEN to user @legacy-agent; existing clients keep working.")
			}
		} else if err != nil {
			return nil, err
		}
		return nil, nil
	}

	if invStore == nil {
		return nil, fmt.Errorf("invitations store required for bootstrap")
	}

	// Reuse an existing active bootstrap invite if present.
	if existing, err := invStore.BootstrapActive(); err != nil {
		return nil, fmt.Errorf("check existing bootstrap invite: %w", err)
	} else if existing != nil {
		return existing, nil
	}

	// Mint a fresh one.
	inv, err := invStore.Create(invitations.CreateParams{
		Role:      invitations.RoleAdmin,
		CreatedBy: invitations.BootstrapCreator,
		ExpiresIn: expiresIn,
		Label:     "first-admin",
	})
	if err != nil {
		return nil, fmt.Errorf("mint bootstrap invite: %w", err)
	}
	return inv, nil
}

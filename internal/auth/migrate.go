package auth

import (
	"errors"
	"fmt"
	"log"
)

// BootstrapOnEmpty handles the one thing auto-setup still does: if a
// fresh instance is booted with AGENTBOARD_AUTH_TOKEN set, create a
// matching `legacy-agent` user so existing clients keep working without
// change. Admin creation has moved to the /api/setup browser flow, so
// this function no longer mints tokens of its own.
//
// Idempotent: no-op once any user exists.
func (s *Store) BootstrapOnEmpty(legacyToken string, logger *log.Logger) error {
	has, err := s.HasAnyUser()
	if err != nil {
		return fmt.Errorf("check users: %w", err)
	}
	if has {
		return nil
	}

	if legacyToken != "" {
		if _, err := s.GetUser("legacy-agent"); errors.Is(err, ErrNotFound) {
			if _, err := s.CreateUser(CreateUserParams{
				Username:  "legacy-agent",
				Kind:      KindAgent,
				CreatedBy: "bootstrap",
			}); err != nil {
				return fmt.Errorf("create legacy agent: %w", err)
			}
			if _, err := s.CreateToken(CreateTokenParams{
				Username:  "legacy-agent",
				TokenHash: HashToken(legacyToken),
				Label:     "legacy",
			}); err != nil {
				return fmt.Errorf("create legacy token: %w", err)
			}
			if logger != nil {
				logger.Printf("Auth: mapped AGENTBOARD_AUTH_TOKEN to user @legacy-agent; existing clients keep working.")
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}

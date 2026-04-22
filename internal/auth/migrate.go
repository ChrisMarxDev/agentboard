package auth

import (
	"errors"
	"fmt"
	"log"
)

// BootstrapOnEmpty ensures the canonical first-run state: an "admin" user
// with one token, plus (optionally) a "legacy-agent" user if the old
// AGENTBOARD_AUTH_TOKEN env var was set.
//
// Idempotent: no-op once any active user exists.
func (s *Store) BootstrapOnEmpty(legacyToken string, logger *log.Logger) error {
	has, err := s.HasAnyUser()
	if err != nil {
		return fmt.Errorf("check users: %w", err)
	}
	if has {
		return nil
	}

	if legacyToken != "" {
		// Reuse if a legacy-agent row somehow already exists; create otherwise.
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

	if _, err := s.CreateUser(CreateUserParams{
		Username:    "admin",
		DisplayName: "Admin",
		Kind:        KindAdmin,
		CreatedBy:   "bootstrap",
	}); err != nil {
		return fmt.Errorf("create admin user: %w", err)
	}
	token, err := GenerateToken()
	if err != nil {
		return fmt.Errorf("generate admin token: %w", err)
	}
	if _, err := s.CreateToken(CreateTokenParams{
		Username:  "admin",
		TokenHash: HashToken(token),
		Label:     "initial",
	}); err != nil {
		return fmt.Errorf("create admin token: %w", err)
	}

	if logger != nil {
		logger.Printf("Auth: initial @admin user created. Copy the token below and paste it into /admin in your browser. If you lose it, run `agentboard admin mint-admin <username>` on the host.")
		logger.Printf("  ADMIN TOKEN: %s", token)
	}
	return nil
}

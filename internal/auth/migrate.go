package auth

import (
	"errors"
	"fmt"
	"log"
)

// MigrateLegacyToken converts the old single-shared-token world to the new
// identity model on startup. Behavior:
//
//   - If any admin identity already exists → no-op (migration already ran, or
//     operator used /setup already).
//   - If legacyToken is empty → no-op (server ran open before, will continue
//     to until /setup is hit).
//   - Otherwise → create one agent identity named "legacy-agent" whose token
//     hash matches the env var. All existing curl commands keep working.
//     Also mint a one-time bootstrap code so the operator can claim admin via
//     the browser; the code is printed via the supplied logger so it shows up
//     in server startup output.
//
// The function is idempotent: re-running with the same legacyToken does not
// duplicate anything, because HasAdmin short-circuits after the first run.
func (s *Store) MigrateLegacyToken(legacyToken string, logger *log.Logger) error {
	has, err := s.HasAdmin()
	if err != nil {
		return fmt.Errorf("check admin exists: %w", err)
	}
	if has {
		return nil
	}

	if legacyToken != "" {
		// Create legacy agent identity if it doesn't already exist.
		if _, err := s.GetIdentityByName("legacy-agent"); errors.Is(err, ErrNotFound) {
			_, err := s.CreateIdentity(CreateIdentityParams{
				Name:       "legacy-agent",
				Kind:       KindAgent,
				TokenHash:  HashToken(legacyToken),
				AccessMode: ModeAllowAll,
				CreatedBy:  "migration",
			})
			if err != nil {
				return fmt.Errorf("create legacy agent: %w", err)
			}
			if logger != nil {
				logger.Printf("Auth migration: the legacy AGENTBOARD_AUTH_TOKEN has been mapped to agent identity \"legacy-agent\". Existing clients keep working unchanged.")
			}
		} else if err != nil {
			return err
		}
	}

	// Mint a bootstrap code so the operator can claim admin from the browser.
	code, _, err := s.CreateBootstrapCode(24*60*60*1e9, "initial-setup") // 24h
	if err != nil {
		return fmt.Errorf("create bootstrap code: %w", err)
	}
	if logger != nil {
		logger.Printf("Auth migration: no admin identity exists. Visit /setup in the browser and enter this one-time code to create the first admin (expires in 24h):")
		logger.Printf("  BOOTSTRAP CODE: %s", code)
		logger.Printf("  (Generate another with `agentboard admin bootstrap-code` if you miss this window.)")
	}
	return nil
}

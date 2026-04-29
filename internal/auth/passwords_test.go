package auth

import (
	"strings"
	"testing"
)

func TestHashPassword_Roundtrip(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Errorf("unexpected hash prefix: %s", hash)
	}
	if !VerifyPassword("correct-horse-battery-staple", hash) {
		t.Error("verify with correct password failed")
	}
	if VerifyPassword("wrong-password", hash) {
		t.Error("verify with wrong password succeeded")
	}
}

func TestHashPassword_TooShort(t *testing.T) {
	_, err := HashPassword("short")
	if err == nil {
		t.Fatal("expected error for too-short password")
	}
	if err != ErrWeakPassword {
		t.Errorf("err = %v, want ErrWeakPassword", err)
	}
}

func TestHashPassword_DifferentSaltsEachCall(t *testing.T) {
	a, _ := HashPassword("same-password-1234")
	b, _ := HashPassword("same-password-1234")
	if a == b {
		t.Error("two HashPassword calls produced identical output — salts collided")
	}
	if !VerifyPassword("same-password-1234", a) || !VerifyPassword("same-password-1234", b) {
		t.Error("both hashes should verify")
	}
}

func TestVerifyPassword_MalformedHashes(t *testing.T) {
	cases := []string{
		"",
		"plaintext",
		"$argon2i$v=19$m=65536,t=1,p=4$saltsaltsalt$keykeykey",  // wrong algo
		"$argon2id$v=18$m=65536,t=1,p=4$saltsaltsalt$keykeykey", // wrong version
		"$argon2id$v=19$m=65536,t=1,p=4$not-base64!$alsobad!",   // bad b64
		"$argon2id$v=19$m=,t=,p=$saltsalt$keykey",               // missing params
		"$argon2id$v=19$$saltsalt$keykey",                       // empty params
	}
	for _, c := range cases {
		if VerifyPassword("anything", c) {
			t.Errorf("VerifyPassword should refuse malformed hash: %q", c)
		}
	}
}

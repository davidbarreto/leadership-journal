package auth

import (
	"strings"
	"testing"
)

// fast keeps unit tests quick; the real parameters are covered by one test.
var fast = params{memoryKiB: 8, iterations: 1, parallelism: 1}

func TestHashAndVerify(t *testing.T) {
	encoded, err := hashWith("correct horse battery", fast)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=8,t=1,p=1$") {
		t.Errorf("encoded = %q, want the standard argon2id format", encoded)
	}
	ok, err := VerifyPassword("correct horse battery", encoded)
	if err != nil || !ok {
		t.Errorf("right password: ok = %v, err = %v", ok, err)
	}
	ok, err = VerifyPassword("wrong horse battery", encoded)
	if err != nil || ok {
		t.Errorf("wrong password: ok = %v, err = %v", ok, err)
	}
}

func TestHash_UsesAFreshSaltEachTime(t *testing.T) {
	a, _ := hashWith("same password!!", fast)
	b, _ := hashWith("same password!!", fast)
	if a == b {
		t.Error("two hashes of the same password are identical: the salt is not random")
	}
}

func TestHashPassword_DefaultParametersRoundTrip(t *testing.T) {
	encoded, err := HashPassword("a long enough passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifyPassword("a long enough passphrase", encoded); err != nil || !ok {
		t.Errorf("round trip failed: ok = %v, err = %v", ok, err)
	}
}

func TestVerifyPassword_RejectsMalformedAndAbusiveHashes(t *testing.T) {
	good, _ := hashWith("whatever password", fast)
	parts := strings.Split(good, "$") // "", argon2id, v=19, params, salt, hash

	tests := []struct {
		name    string
		encoded string
	}{
		{"empty", ""},
		{"not argon2", "$bcrypt$x$y"},
		{"too few parts", "$argon2id$v=19$m=8,t=1,p=1$salt"},
		{"wrong version", "$argon2id$v=18$" + parts[3] + "$" + parts[4] + "$" + parts[5]},
		{"bad params", "$argon2id$v=19$m=x,t=1,p=1$" + parts[4] + "$" + parts[5]},
		{"bad salt encoding", "$argon2id$v=19$" + parts[3] + "$!!!$" + parts[5]},
		{"bad hash encoding", "$argon2id$v=19$" + parts[3] + "$" + parts[4] + "$!!!"},
		// A tampered database must not be able to make us allocate gigabytes.
		{"memory far above the limit", "$argon2id$v=19$m=99999999,t=1,p=1$" + parts[4] + "$" + parts[5]},
		{"iterations far above the limit", "$argon2id$v=19$m=8,t=9999,p=1$" + parts[4] + "$" + parts[5]},
		{"zero parallelism", "$argon2id$v=19$m=8,t=1,p=0$" + parts[4] + "$" + parts[5]},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := VerifyPassword("whatever password", tc.encoded)
			if err == nil || ok {
				t.Errorf("VerifyPassword = %v, %v; want an error and false", ok, err)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("short"); err == nil {
		t.Error("a 5-character password must be rejected")
	}
	if err := ValidatePassword(strings.Repeat("a", 11)); err == nil {
		t.Error("an 11-character password must be rejected")
	}
	if err := ValidatePassword(strings.Repeat("a", 12)); err != nil {
		t.Errorf("a 12-character password is fine: %v", err)
	}
	if err := ValidatePassword(strings.Repeat("a", 1025)); err == nil {
		t.Error("absurdly long passwords must be rejected (they would be a hashing-cost attack)")
	}
}

func TestTokens(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewToken()
	if a == b || len(a) < 40 {
		t.Errorf("tokens must be long and unique: %q %q", a, b)
	}
	if HashToken(a) != HashToken(a) || HashToken(a) == HashToken(b) || HashToken(a) == a {
		t.Error("HashToken must be deterministic, collision-free for distinct tokens, and not the identity")
	}
	if len(HashToken(a)) != 64 {
		t.Errorf("HashToken length = %d, want 64 hex characters", len(HashToken(a)))
	}
}

func TestEqual(t *testing.T) {
	if !Equal("abc", "abc") || Equal("abc", "abd") || Equal("abc", "abcd") || Equal("", "x") {
		t.Error("Equal must compare strings exactly")
	}
}

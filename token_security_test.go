package main

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestGenerateAccountToken(t *testing.T) {
	// Test that tokens are generated correctly
	token := generateAccountToken()

	// Token should not be empty
	if token == "" {
		t.Fatal("Token should not be empty")
	}

	// Token should be base64 encoded (URL safe)
	decoded, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("Token should be valid base64: %v", err)
	}

	// Token should be 64 bytes when decoded (512 bits of entropy)
	if len(decoded) != 64 {
		t.Fatalf("Token should decode to 64 bytes, got %d", len(decoded))
	}

	t.Logf("Generated token: %s", token)
	t.Logf("Token length: %d characters", len(token))
	t.Logf("Decoded length: %d bytes", len(decoded))
}

func TestTokenUniqueness(t *testing.T) {
	// Test that multiple tokens are unique
	tokens := make(map[string]bool)

	for i := 0; i < 1000; i++ {
		token := generateAccountToken()

		if tokens[token] {
			t.Fatalf("Duplicate token generated: %s", token)
		}

		tokens[token] = true
	}

	t.Logf("Generated %d unique tokens", len(tokens))
}

func TestTokenEntropy(t *testing.T) {
	// Test that tokens have sufficient entropy
	token := generateAccountToken()
	decoded, _ := base64.URLEncoding.DecodeString(token)

	// Count unique bytes
	uniqueBytes := make(map[byte]int)
	for _, b := range decoded {
		uniqueBytes[b]++
	}

	// With 64 random bytes, we should have significant variety
	// (not all bytes should be the same)
	if len(uniqueBytes) < 10 {
		t.Fatalf("Token appears to have low entropy: only %d unique bytes", len(uniqueBytes))
	}

	t.Logf("Token has %d unique byte values (out of 256 possible)", len(uniqueBytes))
}

func TestCryptoRandWorks(t *testing.T) {
	b := make([]byte, 32)
	n, err := rand.Read(b)

	if err != nil {
		t.Fatalf("crypto/rand.Read failed: %v", err)
	}

	if n != 32 {
		t.Fatalf("Expected to read 32 bytes, got %d", n)
	}

	allZero := true
	for _, b := range b {
		if b != 0 {
			allZero = false
			break
		}
	}

	if allZero {
		t.Fatal("crypto/rand returned all zeros - this indicates a problem")
	}

	t.Logf("crypto/rand working correctly")
}

func BenchmarkGenerateAccountToken(b *testing.B) {
	for b.Loop() {
		_ = generateAccountToken()
	}
}

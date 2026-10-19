package main

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// Password policy is length-only; characters are not trimmed or normalized.
func validNewPassword(password string) bool {
	return utf8.RuneCountInString(password) >= 6
}

const longPasswordPrefix = "bcrypt-sha256$"

// Preserve existing bcrypt hashes and accept long passwords without truncation.
// Version the prehash format so it cannot be confused with legacy bcrypt input.
func hashPassword(password string) ([]byte, error) {
	if len(password) <= 72 {
		return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	}
	hash, err := bcrypt.GenerateFromPassword(passwordDigest(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return append([]byte(longPasswordPrefix), hash...), nil
}

func passwordDigest(password string) []byte {
	digest := sha256.Sum256([]byte(password))
	return []byte(base64.StdEncoding.EncodeToString(digest[:]))
}

func comparePassword(hash, password string) error {
	if strings.HasPrefix(hash, longPasswordPrefix) {
		return bcrypt.CompareHashAndPassword([]byte(strings.TrimPrefix(hash, longPasswordPrefix)), passwordDigest(password))
	}
	if len(password) > 72 {
		return bcrypt.ErrMismatchedHashAndPassword
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

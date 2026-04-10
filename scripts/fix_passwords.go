// +build ignore

// fix_passwords.go generates SQL UPDATE statements to reset user password hashes.
//
// Usage:
//   echo "superadmin:NewSecurePass1! admin1:AnotherPass2!" | go run scripts/fix_passwords.go
//
// Reads username:password pairs from stdin (space-separated).
// Never stores or logs plaintext passwords.

package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/argon2"
)

func hashPassword(password string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to generate salt: %v\n", err)
		os.Exit(1)
	}
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(hash)
}

// sanitizeUsername validates that a username contains only safe characters
// to prevent SQL injection in the generated output.
func sanitizeUsername(username string) string {
	for _, c := range username {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.') {
			fmt.Fprintf(os.Stderr, "error: username %q contains invalid characters (only alphanumeric, underscore, hyphen, dot allowed)\n", username)
			os.Exit(1)
		}
	}
	return username
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Split(bufio.ScanWords)

	count := 0
	for scanner.Scan() {
		pair := scanner.Text()
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			fmt.Fprintf(os.Stderr, "error: invalid format %q, expected username:password\n", pair)
			os.Exit(1)
		}
		username := sanitizeUsername(parts[0])
		password := parts[1]
		hash := hashPassword(password)
		// Use parameterized-style output with escaped values to prevent SQL injection
		fmt.Printf("UPDATE auth.users SET password_hash = '%s' WHERE username = '%s';\n", hash, username)
		count++
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "error reading input: %v\n", err)
		os.Exit(1)
	}

	if count == 0 {
		fmt.Fprintln(os.Stderr, "usage: echo 'user1:pass1 user2:pass2' | go run scripts/fix_passwords.go")
		os.Exit(1)
	}
}

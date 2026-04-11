package auth

import "testing"

func TestSplitOnce(t *testing.T) {
	tests := []struct {
		input string
		sep   byte
		want  int // number of parts
	}{
		{"abc:def", ':', 2},
		{"abc", ':', 1},
		{"a:b:c", ':', 2}, // only splits on first occurrence
		{"", ':', 1},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := splitOnce(tt.input, tt.sep)
			if len(got) != tt.want {
				t.Errorf("splitOnce(%q, %q) returned %d parts, want %d", tt.input, tt.sep, len(got), tt.want)
			}
		})
	}
}

func TestConstantTimeEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b []byte
		want bool
	}{
		{"equal", []byte{1, 2, 3}, []byte{1, 2, 3}, true},
		{"not equal", []byte{1, 2, 3}, []byte{1, 2, 4}, false},
		{"different lengths", []byte{1, 2}, []byte{1, 2, 3}, false},
		{"empty", []byte{}, []byte{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := constantTimeEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("constantTimeEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHashAndVerifyPassword(t *testing.T) {
	s := &Service{}
	password := "testPassword123!"

	hash, err := s.hashPassword(password)
	if err != nil {
		t.Fatalf("hashPassword() returned error: %v", err)
	}

	if !s.verifyPassword(password, hash) {
		t.Error("verifyPassword() returned false for correct password")
	}

	if s.verifyPassword("wrongPassword", hash) {
		t.Error("verifyPassword() returned true for wrong password")
	}

	if s.verifyPassword(password, "invalid:hash") {
		t.Error("verifyPassword() returned true for invalid hash format")
	}
}

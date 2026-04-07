package validator

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	v10 "github.com/go-playground/validator/v10"
)

var (
	instance *v10.Validate
	once     sync.Once
)

func Get() *v10.Validate {
	once.Do(func() {
		instance = v10.New()
		instance.RegisterValidation("betting_side", validateBettingSide)
		instance.RegisterValidation("betting_price", validateBettingPrice)
		instance.RegisterValidation("upi_id", validateUPIID)
		instance.RegisterValidation("username", validateUsername)
		instance.RegisterValidation("safe_string", validateSafeString)
	})
	return instance
}

func Validate(s interface{}) error {
	err := Get().Struct(s)
	if err == nil {
		return nil
	}
	validationErrors, ok := err.(v10.ValidationErrors)
	if !ok {
		return err
	}
	var msgs []string
	for _, e := range validationErrors {
		msgs = append(msgs, formatError(e))
	}
	return fmt.Errorf("validation failed: %s", strings.Join(msgs, "; "))
}

func formatError(e v10.FieldError) string {
	switch e.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", e.Field())
	case "min":
		return fmt.Sprintf("%s must be at least %s", e.Field(), e.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s", e.Field(), e.Param())
	case "email":
		return fmt.Sprintf("%s must be a valid email", e.Field())
	case "gt":
		return fmt.Sprintf("%s must be greater than %s", e.Field(), e.Param())
	case "lte":
		return fmt.Sprintf("%s must be at most %s", e.Field(), e.Param())
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", e.Field(), e.Param())
	default:
		return fmt.Sprintf("%s failed %s validation", e.Field(), e.Tag())
	}
}

func validateBettingSide(fl v10.FieldLevel) bool {
	side := fl.Field().String()
	return side == "back" || side == "lay"
}

func validateBettingPrice(fl v10.FieldLevel) bool {
	price := fl.Field().Float()
	return price > 1.0 && price <= 1000.0
}

var upiRegex = regexp.MustCompile(`^[a-zA-Z0-9._-]+@[a-zA-Z]{2,}$`)

func validateUPIID(fl v10.FieldLevel) bool {
	return upiRegex.MatchString(fl.Field().String())
}

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]{3,30}$`)

func validateUsername(fl v10.FieldLevel) bool {
	return usernameRegex.MatchString(fl.Field().String())
}

func validateSafeString(fl v10.FieldLevel) bool {
	s := fl.Field().String()
	// Reject HTML/script injection attempts
	dangerous := []string{"<script", "javascript:", "onerror=", "onload=", "<iframe", "<object"}
	lower := strings.ToLower(s)
	for _, d := range dangerous {
		if strings.Contains(lower, d) {
			return false
		}
	}
	return true
}

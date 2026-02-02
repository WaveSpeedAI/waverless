// Package image provides property-based tests for image validation functionality.
// These tests verify universal properties that should hold across all valid inputs.
//
// Feature: image-validation-and-status
// Property 1: Image format validation
// **Validates: Requirements 1.1, 1.2**
package image

import (
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestProperty_ImageFormatValidation tests Property 1: Image Format Validation
//
// Property: For any string input to the Image_Validator, if the string matches
// a valid image reference format (registry/repository:tag or repository:tag),
// the validator SHALL return valid=true; otherwise it SHALL return valid=false
// with a descriptive error message.
//
// Feature: image-validation-and-status, Property 1: Image format validation
// **Validates: Requirements 1.1, 1.2**
func TestProperty_ImageFormatValidation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)
	validator := NewImageValidator(nil)

	// Property 1a: Valid image references should always pass validation
	properties.Property("valid image references pass validation", prop.ForAll(
		func(image string) bool {
			err := validator.ValidateImageFormat(image)
			return err == nil
		},
		genValidImageReference(),
	))

	// Property 1b: Invalid image references should always fail with descriptive error
	properties.Property("invalid image references fail with descriptive error", prop.ForAll(
		func(image string) bool {
			err := validator.ValidateImageFormat(image)
			if err == nil {
				return false // Should have failed
			}
			// Error message should be descriptive (non-empty)
			return len(err.Error()) > 0
		},
		genInvalidImageReference(),
	))

	// Property 1c: Empty strings should always fail validation
	properties.Property("empty strings fail validation", prop.ForAll(
		func(_ int) bool {
			err := validator.ValidateImageFormat("")
			return err != nil && strings.Contains(err.Error(), "cannot be empty")
		},
		gen.Const(0),
	))

	// Property 1d: Strings with leading/trailing whitespace should fail
	properties.Property("whitespace-padded strings fail validation", prop.ForAll(
		func(image string) bool {
			// Add leading whitespace
			paddedImage := " " + image
			err := validator.ValidateImageFormat(paddedImage)
			if err == nil {
				return false
			}
			return strings.Contains(err.Error(), "whitespace")
		},
		genValidRepositoryName(),
	))

	// Property 1e: Validation is deterministic (same input always gives same result)
	properties.Property("validation is deterministic", prop.ForAll(
		func(image string) bool {
			err1 := validator.ValidateImageFormat(image)
			err2 := validator.ValidateImageFormat(image)
			// Both should be nil or both should be non-nil with same message
			if err1 == nil && err2 == nil {
				return true
			}
			if err1 != nil && err2 != nil {
				return err1.Error() == err2.Error()
			}
			return false
		},
		gen.AnyString(),
	))

	properties.TestingRun(t)
}

// TestProperty_ValidImageFormats tests that all valid image format patterns are accepted
//
// Feature: image-validation-and-status, Property 1: Image format validation
// **Validates: Requirements 1.1, 1.3**
func TestProperty_ValidImageFormats(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)
	validator := NewImageValidator(nil)

	// Property: Docker Hub simple names are valid
	properties.Property("docker hub simple names are valid", prop.ForAll(
		func(name string) bool {
			err := validator.ValidateImageFormat(name)
			return err == nil
		},
		genSimpleImageName(),
	))

	// Property: Docker Hub names with namespace are valid
	properties.Property("docker hub names with namespace are valid", prop.ForAll(
		func(namespace, name string) bool {
			image := namespace + "/" + name
			err := validator.ValidateImageFormat(image)
			return err == nil
		},
		genValidRepositoryComponent(),
		genValidRepositoryComponent(),
	))

	// Property: Images with valid tags are valid
	properties.Property("images with valid tags are valid", prop.ForAll(
		func(name, tag string) bool {
			image := name + ":" + tag
			err := validator.ValidateImageFormat(image)
			return err == nil
		},
		genSimpleImageName(),
		genValidTag(),
	))

	// Property: Private registry images are valid
	properties.Property("private registry images are valid", prop.ForAll(
		func(registry, name string) bool {
			image := registry + "/" + name
			err := validator.ValidateImageFormat(image)
			return err == nil
		},
		genValidRegistry(),
		genValidRepositoryComponent(),
	))

	// Property: Images with valid digests are valid
	properties.Property("images with valid digests are valid", prop.ForAll(
		func(name, digest string) bool {
			image := name + "@" + digest
			err := validator.ValidateImageFormat(image)
			return err == nil
		},
		genSimpleImageName(),
		genValidDigest(),
	))

	properties.TestingRun(t)
}

// TestProperty_InvalidImageFormats tests that all invalid image format patterns are rejected
//
// Feature: image-validation-and-status, Property 1: Image format validation
// **Validates: Requirements 1.1, 1.2**
func TestProperty_InvalidImageFormats(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100

	properties := gopter.NewProperties(parameters)
	validator := NewImageValidator(nil)

	// Property: Images with invalid characters fail
	properties.Property("images with invalid characters fail", prop.ForAll(
		func(name string, invalidChar rune) bool {
			image := name + string(invalidChar)
			err := validator.ValidateImageFormat(image)
			return err != nil
		},
		genSimpleImageName(),
		genInvalidCharacter(),
	))

	// Property: Images with uppercase letters in repository fail
	properties.Property("images with uppercase in repository fail", prop.ForAll(
		func(name string) bool {
			if len(name) == 0 {
				return true // Skip empty strings
			}
			// Ensure at least one uppercase letter
			image := strings.ToUpper(name[:1]) + name[1:]
			err := validator.ValidateImageFormat(image)
			return err != nil
		},
		genSimpleImageName(),
	))

	// Property: Images with empty tags fail
	properties.Property("images with empty tags fail", prop.ForAll(
		func(name string) bool {
			image := name + ":"
			err := validator.ValidateImageFormat(image)
			return err != nil && strings.Contains(err.Error(), "tag")
		},
		genSimpleImageName(),
	))

	// Property: Images with invalid digest format fail
	properties.Property("images with invalid digest format fail", prop.ForAll(
		func(name, invalidDigest string) bool {
			image := name + "@" + invalidDigest
			err := validator.ValidateImageFormat(image)
			return err != nil
		},
		genSimpleImageName(),
		genInvalidDigest(),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Generators for valid image components
// ============================================================================

// genValidImageReference generates valid image references
func genValidImageReference() gopter.Gen {
	return gen.OneGenOf(
		genSimpleImageName(),
		genImageWithTag(),
		genImageWithNamespace(),
		// Use well-known registries only to avoid invalid patterns
		genImageWithWellKnownRegistry(),
	)
}

// genSimpleImageName generates simple valid image names (e.g., "nginx", "ubuntu")
func genSimpleImageName() gopter.Gen {
	return genValidRepositoryComponent()
}

// genValidRepositoryComponent generates a valid repository component
// Must be lowercase, start/end with alphanumeric, can contain -, _, .
func genValidRepositoryComponent() gopter.Gen {
	// Generate a valid component: starts with lowercase letter, followed by lowercase alphanumeric
	return gen.RegexMatch(`[a-z][a-z0-9]{0,15}`).SuchThat(func(s string) bool {
		return len(s) >= 1 && len(s) <= 30
	})
}

// genValidRepositoryName generates a valid repository name (may include namespace)
func genValidRepositoryName() gopter.Gen {
	return gen.OneGenOf(
		genValidRepositoryComponent(),
		genNamespaceImagePair(),
	)
}

// genNamespaceImagePair generates namespace/image format
func genNamespaceImagePair() gopter.Gen {
	return gopter.CombineGens(
		genValidRepositoryComponent(),
		genValidRepositoryComponent(),
	).Map(func(vals []interface{}) string {
		return vals[0].(string) + "/" + vals[1].(string)
	})
}

// genImageWithTag generates image:tag format
func genImageWithTag() gopter.Gen {
	return gopter.CombineGens(
		genSimpleImageName(),
		genValidTag(),
	).Map(func(vals []interface{}) string {
		return vals[0].(string) + ":" + vals[1].(string)
	})
}

// genImageWithNamespace generates namespace/image format
func genImageWithNamespace() gopter.Gen {
	return gopter.CombineGens(
		genValidRepositoryComponent(),
		genValidRepositoryComponent(),
	).Map(func(vals []interface{}) string {
		return vals[0].(string) + "/" + vals[1].(string)
	})
}

// genImageWithRegistry generates registry/image format
func genImageWithRegistry() gopter.Gen {
	return gopter.CombineGens(
		genValidRegistry(),
		genValidRepositoryComponent(),
	).Map(func(vals []interface{}) string {
		return vals[0].(string) + "/" + vals[1].(string)
	})
}

// genImageWithWellKnownRegistry generates registry/image format with well-known registries only
func genImageWithWellKnownRegistry() gopter.Gen {
	return gopter.CombineGens(
		gen.OneConstOf("gcr.io", "ghcr.io", "docker.io", "quay.io", "registry.example.com", "localhost"),
		genValidRepositoryComponent(),
	).Map(func(vals []interface{}) string {
		return vals[0].(string) + "/" + vals[1].(string)
	})
}

// genValidTag generates valid image tags
func genValidTag() gopter.Gen {
	return gen.RegexMatch(`[a-zA-Z0-9_][a-zA-Z0-9._-]{0,15}`).SuchThat(func(s string) bool {
		return len(s) >= 1 && len(s) <= 128
	})
}

// genValidRegistry generates valid registry hostnames
func genValidRegistry() gopter.Gen {
	return gen.OneGenOf(
		// Domain-based registries - must not have consecutive hyphens or trailing hyphens before dot
		gen.RegexMatch(`[a-z][a-z0-9]{0,8}[a-z0-9]\.[a-z]{2,4}`),
		// Well-known registries
		gen.OneConstOf("gcr.io", "ghcr.io", "docker.io", "quay.io", "registry.example.com"),
		// localhost
		gen.OneConstOf("localhost", "localhost:5000"),
	)
}

// genValidDigest generates valid image digests
func genValidDigest() gopter.Gen {
	return gen.RegexMatch(`[a-f0-9]{64}`).Map(func(hex string) string {
		return "sha256:" + hex
	})
}

// ============================================================================
// Generators for invalid image components
// ============================================================================

// genInvalidImageReference generates invalid image references
func genInvalidImageReference() gopter.Gen {
	return gen.OneGenOf(
		// Empty string
		gen.Const(""),
		// Whitespace only
		gen.Const("   "),
		// Leading whitespace
		genSimpleImageName().Map(func(s string) string { return " " + s }),
		// Trailing whitespace
		genSimpleImageName().Map(func(s string) string { return s + " " }),
		// Invalid characters
		genImageWithInvalidChar(),
		// Uppercase
		genUppercaseImage(),
		// Empty tag
		genSimpleImageName().Map(func(s string) string { return s + ":" }),
		// Invalid digest
		genImageWithInvalidDigest(),
	)
}

// genImageWithInvalidChar generates an image name with an invalid character
func genImageWithInvalidChar() gopter.Gen {
	return gopter.CombineGens(
		genSimpleImageName(),
		genInvalidCharacter(),
	).Map(func(vals []interface{}) string {
		return vals[0].(string) + string(vals[1].(rune))
	})
}

// genUppercaseImage generates an image name with uppercase letters
func genUppercaseImage() gopter.Gen {
	return genSimpleImageName().SuchThat(func(s string) bool {
		return len(s) > 0
	}).Map(func(s string) string {
		return strings.ToUpper(s[:1]) + s[1:]
	})
}

// genImageWithInvalidDigest generates an image with an invalid digest
func genImageWithInvalidDigest() gopter.Gen {
	return gopter.CombineGens(
		genSimpleImageName(),
		genInvalidDigest(),
	).Map(func(vals []interface{}) string {
		return vals[0].(string) + "@" + vals[1].(string)
	})
}

// genInvalidCharacter generates characters that are invalid in image names
func genInvalidCharacter() gopter.Gen {
	invalidChars := []interface{}{'$', '!', '#', '%', '&', '*', '(', ')', '+', '=', '[', ']', '{', '}', '|', '\\', ';', '"', '\'', '<', '>', ',', '?', '`', '~'}
	return gen.OneConstOf(invalidChars...)
}

// genInvalidDigest generates invalid image digests
func genInvalidDigest() gopter.Gen {
	return gen.OneConstOf(
		// Empty
		"",
		// No colon
		"sha256abc123",
		// Too short hash
		"sha256:abc",
		// Invalid hex characters
		"sha256:xyz123xyz123xyz123xyz123xyz123xyz123",
		// Invalid algorithm
		"123sha:abcdef0123456789abcdef0123456789ab",
	)
}

// ============================================================================
// Helper functions
// ============================================================================

// containsChineseChar checks if a string contains Chinese characters
func containsChineseChar(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}

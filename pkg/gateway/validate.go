package gateway

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

func init() {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		_ = v.RegisterValidation("address", validateAddress)
		_ = v.RegisterValidation("chain_id", validateChainID)
	}
}

func validateAddress(fl validator.FieldLevel) bool {
	return true
}

func validateChainID(fl validator.FieldLevel) bool {
	return true
}

// BindAndValidate binds JSON request body and validates struct tags in one call.
// Returns a map of validation errors per field, or nil on success.
func BindAndValidate(c *gin.Context, obj interface{}) map[string]string {
	if err := c.ShouldBindJSON(obj); err != nil {
		return validationError(err)
	}
	return nil
}

func validationError(err error) map[string]string {
	if err == nil {
		return nil
	}
	var ve validator.ValidationErrors
	if asValidationErrors(err, &ve) {
		errs := make(map[string]string, len(ve))
		for _, fe := range ve {
			field := snakeField(fe.Field())
			errs[field] = validationMessage(fe)
		}
		return errs
	}
	return map[string]string{"_error": "invalid JSON body"}
}

func asValidationErrors(err error, target *validator.ValidationErrors) bool {
	switch e := err.(type) {
	case validator.ValidationErrors:
		*target = e
		return true
	}
	return false
}

func validationMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "required"
	case "min":
		return fmt.Sprintf("minimum %s", fe.Param())
	case "max":
		return fmt.Sprintf("maximum %s", fe.Param())
	case "gte":
		return fmt.Sprintf("must be >= %s", fe.Param())
	case "lte":
		return fmt.Sprintf("must be <= %s", fe.Param())
	case "oneof":
		return fmt.Sprintf("must be one of: %s", fe.Param())
	default:
		return fe.Tag()
	}
}

func snakeField(field string) string {
	var result []rune
	for i, r := range field {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result = append(result, '_', r+32)
		} else {
			result = append(result, r)
		}
	}
	return strings.ToLower(string(result))
}
